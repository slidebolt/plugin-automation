package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/slidebolt/sdk-entities/light_strip"
	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

// PluginAutomationPlugin implements the runner.Plugin interface.
// It manages two virtual devices (core management + automation groups) and keeps
// group entities in sync with the registry via NATS entity-change events,
// debounced to coalesce rapid saves.
type PluginAutomationPlugin struct {
	pctx        runner.PluginContext
	logger      *slog.Logger
	stop        chan struct{}
	done        chan struct{}
	trigRefresh chan struct{} // debounce signal; closed on Stop
}

// Initialize registers the core and groups devices/entities, then runs the first
// group sync. Called once before Start.
func (p *PluginAutomationPlugin) Initialize(ctx runner.PluginContext) (types.Manifest, error) {
	p.pctx = ctx
	p.logger = ctx.Logger

	if ctx.Registry == nil {
		return types.Manifest{}, fmt.Errorf("registry unavailable")
	}
	if err := ctx.Registry.SaveDevice(coreDevice()); err != nil {
		return types.Manifest{}, fmt.Errorf("upsert core device: %w", err)
	}
	for _, ent := range coreEntities() {
		if err := ctx.Registry.SaveEntity(ent); err != nil {
			return types.Manifest{}, fmt.Errorf("upsert core entity %s: %w", ent.ID, err)
		}
	}
	if err := ctx.Registry.SaveDevice(groupsDevice()); err != nil {
		return types.Manifest{}, fmt.Errorf("upsert groups device: %w", err)
	}
	if err := p.refreshGroups(); err != nil {
		p.log().Warn("initialize: initial group refresh failed", "err", err)
	}

	return types.Manifest{
		ID:      "plugin-automation",
		Name:    "Plugin Automation",
		Version: "1.0.0",
		Schemas: types.CoreDomains(),
	}, nil
}

// Start launches the background group-refresh loop and subscribes to registry
// entity-change events so groups are rebuilt reactively rather than on a slow poll.
func (p *PluginAutomationPlugin) Start(_ context.Context) error {
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	p.trigRefresh = make(chan struct{}, 1)

	unsubscribe, err := p.pctx.Registry.OnEntityChanged(func() {
		// Non-blocking send: coalesce rapid bursts into one refresh.
		select {
		case p.trigRefresh <- struct{}{}:
		default:
		}
	})
	if err != nil {
		p.log().Warn("start: could not subscribe to entity changes, falling back to poll", "err", err)
	}

	go p.runRefreshLoop(unsubscribe)
	return nil
}

// Stop signals the background loop and waits for it to exit.
func (p *PluginAutomationPlugin) Stop() error {
	if p.stop != nil {
		close(p.stop)
		<-p.done
	}
	return nil
}

func (p *PluginAutomationPlugin) runRefreshLoop(unsubscribe func()) {
	defer close(p.done)
	defer unsubscribe()

	// Safety-net ticker: catches any edge cases missed by the event subscription.
	safetyticker := time.NewTicker(5 * time.Minute)
	defer safetyticker.Stop()

	// Debounce timer: fires 500 ms after the last entity-change signal.
	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	debouncing := false

	for {
		select {
		case <-p.stop:
			return
		case <-p.trigRefresh:
			if !debouncing {
				debounce.Reset(500 * time.Millisecond)
				debouncing = true
			}
		case <-debounce.C:
			debouncing = false
			if err := p.refreshGroups(); err != nil {
				p.log().Warn("refresh loop: group refresh failed", "err", err)
			}
		case <-safetyticker.C:
			if err := p.refreshGroups(); err != nil {
				p.log().Warn("refresh loop: safety refresh failed", "err", err)
			}
		}
	}
}

// OnReset deletes all persisted devices and entities so a subsequent restart
// re-creates only the canonical set from scratch.
func (p *PluginAutomationPlugin) OnReset() error {
	if p.pctx.Registry == nil {
		return nil
	}
	log := p.log()
	log.Info("reset: wiping all devices and entities")
	devices := p.pctx.Registry.LoadDevices()
	log.Info("reset: wiping devices and entities", "device_count", len(devices))
	for _, dev := range devices {
		entities := p.pctx.Registry.GetEntities(p.pctx.Registry.Namespace(), dev.ID)
		for _, ent := range entities {
			log.Debug("reset: deleting entity", "device_id", dev.ID, "entity_id", ent.ID)
			if err := p.pctx.Registry.DeleteEntity(p.pctx.Registry.Namespace(), dev.ID, ent.ID); err != nil {
				return fmt.Errorf("reset: delete entity %s/%s: %w", dev.ID, ent.ID, err)
			}
		}
		log.Debug("reset: deleting device", "device_id", dev.ID)
		if err := p.pctx.Registry.DeleteDevice(dev.ID); err != nil {
			return fmt.Errorf("reset: delete device %s: %w", dev.ID, err)
		}
	}
	log.Info("reset: complete")
	return p.pctx.Registry.DeleteState()
}

// OnCommand handles commands for virtual group entities owned by plugin-automation.
// Broadcast commands (turn_on, set_rgb, etc.) are routed via CommandQuery fan-out
// by the gateway and never reach here. Only commands excluded from CommandFilter
// — specifically set_segment for light_strip entities — are dispatched here for
// positional translation to the correct physical entity.
func (p *PluginAutomationPlugin) OnCommand(cmd types.Command, entity types.Entity) error {
	switch entity.Domain {
	case light_strip.Type:
		return p.handleStripCommand(cmd, entity)
	default:
		p.log().Error("OnCommand received unexpected domain — this is a bug", "domain", entity.Domain, "entity_id", entity.ID)
		return nil
	}
}

func (p *PluginAutomationPlugin) handleStripCommand(cmd types.Command, entity types.Entity) error {
	var c struct {
		Type    string `json:"type"`
		Segment *struct {
			Index      int   `json:"index"`
			RGB        []int `json:"rgb,omitempty"`
			Brightness *int  `json:"brightness,omitempty"`
		} `json:"segment,omitempty"`
	}
	if err := json.Unmarshal(cmd.Payload, &c); err != nil {
		return err
	}
	if c.Type != light_strip.ActionSetSegment {
		// Broadcast commands (filtered out of CommandFilter) should not reach here,
		// but guard defensively.
		return nil
	}
	if c.Segment == nil {
		return fmt.Errorf("set_segment: segment is required")
	}

	members, err := loadStripMembers(entity)
	if err != nil {
		return err
	}

	var target *stripMember
	for i := range members {
		if members[i].Index == c.Segment.Index {
			target = &members[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("set_segment: no member at index %d", c.Segment.Index)
	}

	var payload json.RawMessage
	if len(c.Segment.RGB) == 3 {
		payload, err = json.Marshal(map[string]any{"type": "set_rgb", "rgb": c.Segment.RGB})
	} else if c.Segment.Brightness != nil {
		payload, err = json.Marshal(map[string]any{"type": "set_brightness", "brightness": *c.Segment.Brightness})
	} else {
		return fmt.Errorf("set_segment: must provide rgb or brightness")
	}
	if err != nil {
		return err
	}

	return p.pctx.Commands.SendCommand(types.Command{
		ID:         cmd.ID + "-seg",
		PluginID:   target.PluginID,
		DeviceID:   target.DeviceID,
		EntityID:   target.EntityID,
		EntityType: "light",
		Payload:    payload,
	})
}

// loadStripMembers unmarshals the strip_members meta blob from a light_strip entity.
func loadStripMembers(entity types.Entity) ([]stripMember, error) {
	raw, ok := entity.Meta["strip_members"]
	if !ok {
		return nil, fmt.Errorf("entity %s has no strip_members meta", entity.ID)
	}
	var members []stripMember
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, fmt.Errorf("unmarshal strip_members for %s: %w", entity.ID, err)
	}
	return members, nil
}

// refreshGroups queries the registry for PluginAutomation labels and upserts
// the derived group entities into the groups device.
func (p *PluginAutomationPlugin) refreshGroups() error {
	var groups []types.Entity
	if p.pctx.Registry != nil {
		matches := p.pctx.Registry.FindEntities(types.SearchQuery{Pattern: "*"})
		var err error
		groups, err = buildAutoGroups(matches, p.log())
		if err != nil {
			return err
		}
	}
	for _, ent := range groups {
		if err := p.pctx.Registry.SaveEntity(ent); err != nil {
			p.log().Warn("refresh groups: upsert failed", "entity_id", ent.ID, "err", err)
		}
	}
	return nil
}

func (p *PluginAutomationPlugin) log() *slog.Logger {
	if p.logger != nil {
		return p.logger
	}
	return slog.Default()
}
