//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
	integrationtesting "github.com/slidebolt/sdk-integration-testing"
	"github.com/slidebolt/sdk-types"
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

// TestIntegration_SetSegment_Dispatch boots plugin-automation alongside
// plugin-test-clean. It seeds two light entities on plugin-test-clean with
// PluginAutomation:BasementLS labels and positional meta blobs, waits for
// plugin-automation to create the virtual light_strip entity, sends a
// set_segment command to the strip, and asserts that the translated set_rgb
// command arrives on NATS for the correct physical entity.
// TestIntegration_SetPanel_Dispatch boots plugin-automation alongside
// plugin-test-clean. It seeds two light entities on plugin-test-clean with
// PluginAutomation:OfficeP labels and panel_id meta blobs, waits for
// plugin-automation to create the virtual light_panel entity, sends a
// set_panel command to the panel, and asserts that the translated set_rgb
// command arrives on NATS for the correct physical entity (matched by panel_id,
// not positional index).
func TestIntegration_SetPanel_Dispatch(t *testing.T) {
	const (
		leafPluginID   = "plugin-test-clean"
		leafDeviceID   = "test-panel-device"
		entity0ID      = "panel-light-0"
		entity1ID      = "panel-light-1"
		groupName      = "OfficeP"
		panelEntityID  = "group-officep"
		panelDeviceID  = "groups"
		panel0ID       = 100
		panel1ID       = 200
	)

	s := integrationtesting.NewMulti(t,
		integrationtesting.PluginSpec{Module: "github.com/slidebolt/plugin-automation", Dir: "."},
		integrationtesting.PluginSpec{Module: "github.com/slidebolt/plugin-test-clean", Dir: "../plugin-test-clean"},
	)
	s.RequirePlugin(pluginID)
	s.RequirePlugin(leafPluginID)

	// --- Step 1: seed two light entities with panel_id meta ---

	metaFor := func(panelID int) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"domain": "light_panel", "panel_id": panelID})
		return raw
	}

	seedEntity := func(entityID string, panelID int) {
		entity := types.Entity{
			ID:       entityID,
			DeviceID: leafDeviceID,
			Domain:   "light",
			Labels: map[string][]string{
				"PluginAutomation": {groupName},
			},
			Meta: map[string]json.RawMessage{
				fmt.Sprintf("PluginAutomation:%s", groupName): metaFor(panelID),
			},
		}
		path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities", leafPluginID, leafDeviceID)
		if err := s.PostJSON(path, entity, nil); err != nil {
			t.Fatalf("seed entity %s: %v", entityID, err)
		}
	}

	seedEntity(entity0ID, panel0ID)
	seedEntity(entity1ID, panel1ID)
	t.Log("seeded panel leaf entities")

	// --- Step 2: subscribe to NATS ---

	nc, err := nats.Connect(s.NATSURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()

	cmdCh := make(chan types.Command, 8)
	sub, err := nc.Subscribe(
		"slidebolt.rpc."+leafPluginID+".command",
		func(msg *nats.Msg) {
			var cmd types.Command
			if json.Unmarshal(msg.Data, &cmd) == nil {
				cmdCh <- cmd
			}
		},
	)
	if err != nil {
		t.Fatalf("nats subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// --- Step 3: wait for plugin-automation to create the virtual panel entity ---

	t.Log("waiting for plugin-automation to create virtual panel entity...")
	found := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities", pluginID, panelDeviceID)
		if err := s.GetJSON(path, &entities); err != nil {
			return false
		}
		for _, e := range entities {
			if id, _ := e["id"].(string); id == panelEntityID {
				domain, _ := e["domain"].(string)
				return domain == "light_panel"
			}
		}
		return false
	})
	if !found {
		t.Fatal("timed out waiting for virtual light_panel entity to appear")
	}
	t.Logf("virtual panel entity %q found", panelEntityID)

	// --- Step 4: send set_panel targeting panel_id=200 (entity1) ---

	cmdPath := fmt.Sprintf("/api/plugins/%s/devices/%s/entities/%s/commands",
		pluginID, panelDeviceID, panelEntityID)
	payload := map[string]any{
		"type":  "set_panel",
		"panel": map[string]any{"id": panel1ID, "rgb": []int{0, 128, 255}},
	}
	if err := s.PostJSON(cmdPath, payload, nil); err != nil {
		t.Fatalf("send set_panel: %v", err)
	}
	t.Log("set_panel sent to virtual panel entity")

	// --- Step 5: assert set_rgb dispatched to entity-1 ---

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	var got types.Command
	select {
	case got = <-cmdCh:
	case <-deadline.C:
		t.Fatal("timed out waiting for dispatched set_rgb command on NATS")
	}

	if got.EntityID != entity1ID {
		t.Errorf("expected command for entity %q (panel_id=%d), got %q", entity1ID, panel1ID, got.EntityID)
	}

	var cmdPayload struct {
		Type string `json:"type"`
		RGB  []int  `json:"rgb"`
	}
	if err := json.Unmarshal(got.Payload, &cmdPayload); err != nil {
		t.Fatalf("unmarshal dispatched payload: %v", err)
	}
	if cmdPayload.Type != "set_rgb" {
		t.Errorf("expected command type set_rgb, got %q", cmdPayload.Type)
	}
	if len(cmdPayload.RGB) != 3 || cmdPayload.RGB[1] != 128 {
		t.Errorf("expected rgb=[0,128,255], got %v", cmdPayload.RGB)
	}

	// Assert entity-0 did NOT receive a command.
	select {
	case extra := <-cmdCh:
		if extra.EntityID == entity0ID {
			t.Errorf("entity-0 (panel_id=%d) should NOT have received a command but did: %+v", panel0ID, extra)
		}
	default:
	}

	t.Logf("set_rgb dispatched correctly by panel_id: entity=%s rgb=%v", got.EntityID, cmdPayload.RGB)
}

func TestIntegration_SetSegment_Dispatch(t *testing.T) {
	const (
		leafPluginID  = "plugin-test-clean"
		leafDeviceID  = "test-strip-device"
		entity0ID     = "strip-light-0"
		entity1ID     = "strip-light-1"
		groupName     = "BasementLS"
		stripEntityID = "group-basementls"
		stripDeviceID = "groups"
	)

	s := integrationtesting.NewMulti(t,
		integrationtesting.PluginSpec{Module: "github.com/slidebolt/plugin-automation", Dir: "."},
		integrationtesting.PluginSpec{Module: "github.com/slidebolt/plugin-test-clean", Dir: "../plugin-test-clean"},
	)
	s.RequirePlugin(pluginID)
	s.RequirePlugin(leafPluginID)

	// --- Step 1: seed two light entities on plugin-test-clean ---

	metaFor := func(index int) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"domain": "light_strip", "index": index})
		return raw
	}

	seedEntity := func(entityID string, index int) {
		entity := types.Entity{
			ID:       entityID,
			DeviceID: leafDeviceID,
			Domain:   "light",
			Labels: map[string][]string{
				"PluginAutomation": {groupName},
			},
			Meta: map[string]json.RawMessage{
				fmt.Sprintf("PluginAutomation:%s", groupName): metaFor(index),
			},
		}
		path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities", leafPluginID, leafDeviceID)
		if err := s.PostJSON(path, entity, nil); err != nil {
			t.Fatalf("seed entity %s: %v", entityID, err)
		}
	}

	seedEntity(entity0ID, 0)
	seedEntity(entity1ID, 1)
	t.Log("seeded leaf entities")

	// --- Step 2: subscribe to NATS *before* sending the command ---

	nc, err := nats.Connect(s.NATSURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()

	cmdCh := make(chan types.Command, 8)
	sub, err := nc.Subscribe(
		"slidebolt.rpc."+leafPluginID+".command",
		func(msg *nats.Msg) {
			var cmd types.Command
			if json.Unmarshal(msg.Data, &cmd) == nil {
				cmdCh <- cmd
			}
		},
	)
	if err != nil {
		t.Fatalf("nats subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// --- Step 3: wait for plugin-automation to build the virtual strip ---
	// refreshGroups is triggered reactively via NATS entity-change events;
	// with a 500 ms debounce it should complete within a couple of seconds.

	t.Log("waiting for plugin-automation to create virtual strip entity...")
	found := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities", pluginID, stripDeviceID)
		if err := s.GetJSON(path, &entities); err != nil {
			return false
		}
		for _, e := range entities {
			if id, _ := e["id"].(string); id == stripEntityID {
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatal("timed out waiting for virtual light_strip entity to appear")
	}
	t.Logf("virtual strip entity %q found", stripEntityID)

	// --- Step 4: send set_segment targeting index 0 ---

	cmdPath := fmt.Sprintf("/api/plugins/%s/devices/%s/entities/%s/commands",
		pluginID, stripDeviceID, stripEntityID)
	payload := map[string]any{
		"type": "set_segment",
		"segment": map[string]any{
			"index": 0,
			"rgb":   []int{255, 0, 0},
		},
	}
	if err := s.PostJSON(cmdPath, payload, nil); err != nil {
		t.Fatalf("send set_segment: %v", err)
	}
	t.Log("set_segment sent to virtual strip")

	// --- Step 5: assert set_rgb dispatched to entity-0 within 5 s ---

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	var got types.Command
	select {
	case got = <-cmdCh:
	case <-deadline.C:
		t.Fatal("timed out waiting for dispatched set_rgb command on NATS")
	}

	// Verify the command targets the correct leaf entity.
	if got.EntityID != entity0ID {
		t.Errorf("expected command for entity %q, got %q", entity0ID, got.EntityID)
	}
	if got.DeviceID != leafDeviceID {
		t.Errorf("expected device %q, got %q", leafDeviceID, got.DeviceID)
	}

	var cmdPayload struct {
		Type string `json:"type"`
		RGB  []int  `json:"rgb"`
	}
	if err := json.Unmarshal(got.Payload, &cmdPayload); err != nil {
		t.Fatalf("unmarshal dispatched payload: %v", err)
	}
	if cmdPayload.Type != "set_rgb" {
		t.Errorf("expected command type set_rgb, got %q", cmdPayload.Type)
	}
	if len(cmdPayload.RGB) != 3 || cmdPayload.RGB[0] != 255 || cmdPayload.RGB[1] != 0 || cmdPayload.RGB[2] != 0 {
		t.Errorf("expected rgb=[255,0,0], got %v", cmdPayload.RGB)
	}

	// Assert entity-1 did NOT receive a command during the assertion window.
	select {
	case extra := <-cmdCh:
		if extra.EntityID == entity1ID {
			t.Errorf("entity-1 should NOT have received a command but did: %+v", extra)
		}
	default:
		// nothing — correct
	}

	t.Logf("set_rgb dispatched correctly: entity=%s rgb=%v", got.EntityID, cmdPayload.RGB)
}
