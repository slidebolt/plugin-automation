package main

import (
	"log"

	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

type PluginAutomationPlugin struct{}

func (p *PluginAutomationPlugin) OnInitialize(config runner.Config, state types.Storage) (types.Manifest, types.Storage) {
	return types.Manifest{ID: "plugin-automation", Name: "Plugin Automation", Version: "1.0.0"}, state
}

func (p *PluginAutomationPlugin) OnReady()                       {}
func (p *PluginAutomationPlugin) OnHealthCheck() (string, error) { return "perfect", nil }
func (p *PluginAutomationPlugin) OnStorageUpdate(current types.Storage) (types.Storage, error) {
	return current, nil
}

func (p *PluginAutomationPlugin) OnDeviceCreate(dev types.Device) (types.Device, error) {
	dev.Config = types.Storage{Meta: "plugin-automation-metadata"}
	return dev, nil
}
func (p *PluginAutomationPlugin) OnDeviceUpdate(dev types.Device) (types.Device, error) { return dev, nil }
func (p *PluginAutomationPlugin) OnDeviceDelete(id string) error                        { return nil }
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

func (p *PluginAutomationPlugin) OnCommand(cmd types.Command, entity types.Entity) (types.Entity, error) {
	return entity, nil
}
func (p *PluginAutomationPlugin) OnEvent(evt types.Event, entity types.Entity) (types.Entity, error) {
	return entity, nil
}

func main() {
	if err := runner.NewRunner(&PluginAutomationPlugin{}).Run(); err != nil {
		log.Fatal(err)
	}
}
