package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

type PluginAutomationPlugin struct {
	sink runner.EventSink
}

func (p *PluginAutomationPlugin) OnInitialize(config runner.Config, state types.Storage) (types.Manifest, types.Storage) {
	p.sink = config.EventSink
	return types.Manifest{ID: "plugin-automation", Name: "Plugin Automation", Version: "1.0.0"}, state
}

func (p *PluginAutomationPlugin) OnReady() {}
func (p *PluginAutomationPlugin) WaitReady(ctx context.Context) error {
	return nil
}

func (p *PluginAutomationPlugin) OnShutdown()                    {}
func (p *PluginAutomationPlugin) OnHealthCheck() (string, error) { return "perfect", nil }
func (p *PluginAutomationPlugin) OnStorageUpdate(current types.Storage) (types.Storage, error) {
	return current, nil
}

func (p *PluginAutomationPlugin) OnDeviceCreate(dev types.Device) (types.Device, error) {
	return dev, nil
}
func (p *PluginAutomationPlugin) OnDeviceUpdate(dev types.Device) (types.Device, error) {
	return dev, nil
}
func (p *PluginAutomationPlugin) OnDeviceDelete(id string) error { return nil }
func (p *PluginAutomationPlugin) OnDevicesList(current []types.Device) ([]types.Device, error) {
	return current, nil
}
func (p *PluginAutomationPlugin) OnDeviceSearch(q types.SearchQuery, res []types.Device) ([]types.Device, error) {
	return res, nil
}

func (p *PluginAutomationPlugin) OnEntityCreate(e types.Entity) (types.Entity, error) { return e, nil }
func (p *PluginAutomationPlugin) OnEntityUpdate(e types.Entity) (types.Entity, error) { return e, nil }
func (p *PluginAutomationPlugin) OnEntityDelete(d, e string) error                    { return nil }
func (p *PluginAutomationPlugin) OnEntitiesList(d string, c []types.Entity) ([]types.Entity, error) {
	return c, nil
}

func (p *PluginAutomationPlugin) OnCommandTyped(req types.CommandRequest[types.GenericPayload], entity types.Entity) (types.Entity, error) {
	// Automation plugin: commands are handled by Lua scripts, not the plugin itself.
	return entity, nil
}

func (p *PluginAutomationPlugin) OnEventTyped(evt types.EventTyped[types.GenericPayload], entity types.Entity) (types.Entity, error) {
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

func main() {
	if err := runner.NewRunner(&PluginAutomationPlugin{}).Run(); err != nil {
		log.Fatal(err)
	}
}
