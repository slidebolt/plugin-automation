package main

import (
	"context"
	"testing"

	regsvc "github.com/slidebolt/registry"
	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

func newTestPlugin(t *testing.T) (*PluginAutomationPlugin, *regsvc.Registry) {
	t.Helper()
	p := &PluginAutomationPlugin{}
	reg := regsvc.RegistryService("plugin-automation", regsvc.WithPersist(regsvc.PersistNever))
	_, err := p.Initialize(runner.PluginContext{Registry: reg})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return p, reg
}

func TestInitializeRegistersCoreAndGroups(t *testing.T) {
	_, reg := newTestPlugin(t)

	if _, ok := reg.LoadDevice("plugin-automation"); !ok {
		t.Fatal("expected plugin-automation core device")
	}
	if _, ok := reg.LoadDevice("groups"); !ok {
		t.Fatal("expected groups device")
	}
	if _, ok := reg.GetEntity("plugin-automation", "plugin-automation", "health"); !ok {
		t.Fatal("expected health core entity")
	}
}

func TestInitializeManifest(t *testing.T) {
	p := &PluginAutomationPlugin{}
	reg := regsvc.RegistryService("plugin-automation", regsvc.WithPersist(regsvc.PersistNever))
	manifest, err := p.Initialize(runner.PluginContext{Registry: reg})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if manifest.ID != "plugin-automation" {
		t.Fatalf("manifest.ID=%q", manifest.ID)
	}
}

func TestStartStop(t *testing.T) {
	p, _ := newTestPlugin(t)
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestOnResetClearsRegistry(t *testing.T) {
	p, reg := newTestPlugin(t)

	_ = reg.SaveEntity(types.Entity{ID: "group-basement", DeviceID: "groups", Domain: "switch"})
	_ = reg.SaveState(types.Storage{Data: []byte(`{"x":1}`)})

	if err := p.OnReset(); err != nil {
		t.Fatalf("OnReset: %v", err)
	}
	if len(reg.LoadDevices()) != 0 {
		t.Fatalf("expected devices cleared")
	}
	if _, ok := reg.LoadState(); ok {
		t.Fatalf("expected state cleared")
	}
}

func TestBuildAutoGroups(t *testing.T) {
	entities := []types.Entity{
		{
			ID:       "e1",
			DeviceID: "d1",
			PluginID: "plugin-zwave",
			Domain:   "switch",
			Actions:  []string{"turn_on", "turn_off"},
			Labels:   map[string][]string{"PluginAutomation": {"Basement"}},
		},
		{
			ID:       "group-basement",
			DeviceID: "groups",
			PluginID: "plugin-automation",
			Labels:   map[string][]string{"PluginAutomation": {"Basement"}},
		},
	}
	groups, err := buildAutoGroups(entities, nil)
	if err != nil {
		t.Fatalf("buildAutoGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].ID != "group-basement" {
		t.Fatalf("unexpected group id %q", groups[0].ID)
	}
}
