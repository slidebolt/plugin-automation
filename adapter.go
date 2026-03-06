package main

import (
	"context"
	"encoding/json"
	"time"

	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

// PluginAutomationPlugin implements the runner.Plugin interface for the automation plugin.
// This plugin provides virtual switch capabilities within the Slidebolt ecosystem.
type PluginAutomationPlugin struct {
	sink runner.EventSink
}

// OnInitialize is called when the plugin is initialized by the runner.
func (p *PluginAutomationPlugin) OnInitialize(config runner.Config, state types.Storage) (types.Manifest, types.Storage) {
	p.sink = config.EventSink
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

// OnStorageUpdate is called when the plugin's storage is updated.
func (p *PluginAutomationPlugin) OnStorageUpdate(current types.Storage) (types.Storage, error) {
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

// OnDevicesList is called to list all devices managed by this plugin.
func (p *PluginAutomationPlugin) OnDevicesList(current []types.Device) ([]types.Device, error) {
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

// OnEntitiesList is called to list all entities for a device.
func (p *PluginAutomationPlugin) OnEntitiesList(d string, c []types.Entity) ([]types.Entity, error) {
	return runner.EnsureCoreEntities("plugin-automation", d, c), nil
}

// OnCommand handles commands sent to entities.
// For the automation plugin, commands are handled by Lua scripts, not the plugin itself.
func (p *PluginAutomationPlugin) OnCommand(req types.Command, entity types.Entity) (types.Entity, error) {
	return entity, nil
}

// OnEvent handles events and updates entity state accordingly.
func (p *PluginAutomationPlugin) OnEvent(evt types.Event, entity types.Entity) (types.Entity, error) {
	raw, err := json.Marshal(evt.Payload)
	if err != nil {
		return entity, err
	}
	entity.Data.Reported = raw
	entity.Data.Effective = raw
	entity.Data.SyncStatus = "in_sync"
	entity.Data.UpdatedAt = time.Now().UTC()
	return entity, nil
}
