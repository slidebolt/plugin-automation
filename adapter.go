package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

// PluginAutomationPlugin implements the runner.Plugin interface.
// It manages two virtual devices (core management + automation groups) and keeps
// group entities in sync with the registry via a background refresh loop.
type PluginAutomationPlugin struct {
	pctx   runner.PluginContext
	logger *slog.Logger
	stop   chan struct{}
	done   chan struct{}
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

// Start launches the background group-refresh loop.
func (p *PluginAutomationPlugin) Start(_ context.Context) error {
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	go p.runRefreshLoop()
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

func (p *PluginAutomationPlugin) runRefreshLoop() {
	defer close(p.done)
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			if err := p.refreshGroups(); err != nil {
				p.log().Warn("refresh loop: group refresh failed", "err", err)
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

// OnCommand is a pass-through. Group entity commands are handled by the gateway's
// virtual fan-out routing via the virtual_source_query label.
func (p *PluginAutomationPlugin) OnCommand(_ types.Command, _ types.Entity) error {
	return nil
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
