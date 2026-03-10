package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	pluginerrors "github.com/slidebolt/plugin-automation/internal/errors"
	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

// PluginAutomationPlugin implements the runner.Plugin interface for the automation plugin.
// This plugin provides virtual switch capabilities within the Slidebolt ecosystem.
type PluginAutomationPlugin struct {
	sink   runner.EventSink
	master runner.RegistryCache
}

// OnInitialize is called when the plugin is initialized by the runner.
func (p *PluginAutomationPlugin) OnInitialize(config runner.Config, state types.Storage) (types.Manifest, types.Storage) {
	p.sink = config.EventSink
	p.master = config.RegistryCache
	return types.Manifest{
		ID:      "plugin-automation",
		Name:    "Plugin Automation",
		Version: "1.0.0",
		Schemas: types.CoreDomains(),
	}, state
}

// OnReady is called when the plugin is ready to start processing.
func (p *PluginAutomationPlugin) OnReady() {}

// WaitReady blocks until the plugin is ready.
func (p *PluginAutomationPlugin) WaitReady(ctx context.Context) error {
	return nil
}

// OnShutdown is called when the plugin is being shut down.
func (p *PluginAutomationPlugin) OnShutdown() {}

// OnHealthCheck returns the health status of the plugin.
func (p *PluginAutomationPlugin) OnHealthCheck() (string, error) {
	return "perfect", nil
}

// OnConfigUpdate is called when the plugin's storage is updated.
func (p *PluginAutomationPlugin) OnConfigUpdate(current types.Storage) (types.Storage, error) {
	return current, nil
}

// OnDeviceCreate is called when a new device is created.
func (p *PluginAutomationPlugin) OnDeviceCreate(dev types.Device) (types.Device, error) {
	return dev, nil
}

// OnDeviceUpdate is called when a device is updated.
func (p *PluginAutomationPlugin) OnDeviceUpdate(dev types.Device) (types.Device, error) {
	return dev, nil
}

// OnDeviceDelete is called when a device is deleted.
func (p *PluginAutomationPlugin) OnDeviceDelete(id string) error {
	return nil
}

// OnDeviceDiscover is called to list all devices managed by this plugin.
func (p *PluginAutomationPlugin) OnDeviceDiscover(current []types.Device) ([]types.Device, error) {
	return runner.EnsureCoreDevice("plugin-automation", current), nil
}

// OnDeviceSearch is called to search for devices.
func (p *PluginAutomationPlugin) OnDeviceSearch(q types.SearchQuery, res []types.Device) ([]types.Device, error) {
	return res, nil
}

// OnEntityCreate is called when a new entity is created.
func (p *PluginAutomationPlugin) OnEntityCreate(e types.Entity) (types.Entity, error) {
	return e, nil
}

// OnEntityUpdate is called when an entity is updated.
func (p *PluginAutomationPlugin) OnEntityUpdate(e types.Entity) (types.Entity, error) {
	return e, nil
}

// OnEntityDelete is called when an entity is deleted.
func (p *PluginAutomationPlugin) OnEntityDelete(d, e string) error {
	return nil
}

// OnEntityDiscover is called to list all entities for a device.
func (p *PluginAutomationPlugin) OnEntityDiscover(d string, c []types.Entity) ([]types.Entity, error) {
	if d == "plugin-automation" && p.master != nil {
		groups := p.buildAutoGroups()
		c = append(c, groups...)
	}
	return runner.EnsureCoreEntities("plugin-automation", d, c), nil
}

func (p *PluginAutomationPlugin) buildAutoGroups() []types.Entity {
	matches, err := p.master.FindEntities(types.SearchQuery{Pattern: "*"})
	if err != nil {
		return nil
	}

	type sample struct {
		domain  string
		actions []string
	}
	groupSamples := map[string]sample{}
	for _, m := range matches {
		vals := m.Labels["PluginAutomation"]
		if len(vals) == 0 {
			continue
		}
		// Do not include generated automation groups in the source set.
		if m.PluginID == "plugin-automation" && m.DeviceID == "plugin-automation" {
			continue
		}
		for _, g := range vals {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			if _, ok := groupSamples[g]; !ok {
				groupSamples[g] = sample{
					domain:  strings.TrimSpace(m.Domain),
					actions: append([]string(nil), m.Actions...),
				}
			}
		}
	}

	if len(groupSamples) == 0 {
		return nil
	}
	names := make([]string, 0, len(groupSamples))
	for name := range groupSamples {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]types.Entity, 0, len(names))
	for _, name := range names {
		s := groupSamples[name]
		domain := s.domain
		if domain == "" {
			domain = "switch"
		}
		actions := append([]string(nil), s.actions...)
		if len(actions) == 0 {
			actions = []string{"turn_on", "turn_off"}
		}
		query := fmt.Sprintf("?label=PluginAutomation:%s", name)
		out = append(out, types.Entity{
			ID:        "group-" + normalizeID(name),
			DeviceID:  "plugin-automation",
			Domain:    domain,
			LocalName: name,
			Actions:   actions,
			Labels: map[string][]string{
				"Group":                {name},
				"virtual_source_query": {query},
			},
			Data: types.EntityData{
				SyncStatus: types.SyncStatusSynced,
				UpdatedAt:  time.Now().UTC(),
			},
		})
	}
	return out
}

func normalizeID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "unnamed"
	}
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "unnamed"
	}
	return id
}

// OnCommand handles commands sent to entities.
// For the automation plugin, commands are handled by Lua scripts, not the plugin itself.
// This method ensures proper error handling and sync status updates.
func (p *PluginAutomationPlugin) OnCommand(req types.Command, entity types.Entity) (types.Entity, error) {
	// The automation plugin passes commands to Lua scripts.
	// If there was an error processing the command, it would be reflected here.
	// Currently, this is a passthrough as Lua handles the actual command execution.
	return entity, nil
}

// OnEvent handles events and updates entity state accordingly.
// On failure, it updates the SyncStatus to "failed" and includes error details
// in the Reported state for the Slidebolt UI to display.
func (p *PluginAutomationPlugin) OnEvent(evt types.Event, entity types.Entity) (types.Entity, error) {
	raw, err := json.Marshal(evt.Payload)
	if err != nil {
		// Wrap the error with structured error type
		pluginErr := pluginerrors.NewInvalidPayloadError(err)

		// Update entity with error state
		entity.Data.SyncStatus = types.SyncStatusFailed
		entity.Data.UpdatedAt = time.Now().UTC()

		// Create error state by merging existing reported data with error info
		var reportedMap map[string]interface{}
		if len(entity.Data.Reported) > 0 {
			json.Unmarshal(entity.Data.Reported, &reportedMap)
		}
		if reportedMap == nil {
			reportedMap = make(map[string]interface{})
		}

		// Add error information to the reported state
		for k, v := range pluginErr.ToStateField() {
			reportedMap[k] = v
		}

		entity.Data.Reported, _ = json.Marshal(reportedMap)
		entity.Data.Effective = entity.Data.Reported

		return entity, pluginErr
	}

	// Success case - payload marshaled successfully
	entity.Data.Reported = raw
	entity.Data.Effective = raw
	entity.Data.SyncStatus = types.SyncStatusSynced
	entity.Data.UpdatedAt = time.Now().UTC()
	return entity, nil
}
