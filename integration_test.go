package main

import (
	"testing"

	"github.com/slidebolt/sdk-integration-testing"
)

const pluginID = "plugin-automation"

func TestIntegration_PluginRegisters(t *testing.T) {
	s := integrationtesting.New(t, "github.com/slidebolt/plugin-automation", ".")
	s.RequirePlugin(pluginID)

	plugins, err := s.Plugins()
	if err != nil {
		t.Fatalf("GET /api/plugins: %v", err)
	}
	reg, ok := plugins[pluginID]
	if !ok {
		t.Fatalf("plugin %q not in registry", pluginID)
	}
	t.Logf("registered: id=%s name=%s", pluginID, reg.Manifest.Name)
}

func TestIntegration_CoreDevicesPresent(t *testing.T) {
	s := integrationtesting.New(t, "github.com/slidebolt/plugin-automation", ".")
	s.RequirePlugin(pluginID)

	var devices []map[string]any
	if err := s.GetJSON("/api/plugins/"+pluginID+"/devices", &devices); err != nil {
		t.Fatalf("GET devices: %v", err)
	}
	if len(devices) == 0 {
		t.Error("expected at least one core device registered on Start")
	}
	t.Logf("devices registered: %d", len(devices))
}
