//go:build integration

//go:build test_multi

// Multi-plugin integration tests for plugin-automation.
// These require no hardware and are safe to run in CI.
//
// Run standalone:
//
//	cd raw/plugin-automation
//	go test -tags test_multi -v -count=1 ./...
//
// Or from testrunner (full platform stack):
//
//	cd raw/testrunner && mage test
//
// How this works:
//
//	TestMain calls RunAll with plugin-automation + plugin-test-clean.
//	plugin-test-clean acts as a controllable leaf plugin — we seed devices
//	and entities onto it via the gateway API, which triggers plugin-automation
//	to react and create virtual group/strip entities.
//
//	All Test* functions call GetSuite(t) to get the shared running stack.
//	The stack starts once in TestMain and is reused across every test.
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

// TestMain boots one shared stack for all test_multi tests in this file.
// plugin-test-clean is included as the controllable leaf: we create devices
// and entities on it via the gateway API to drive plugin-automation's logic.
func TestMain(m *testing.M) {
	integrationtesting.RunAll(m, []integrationtesting.PluginSpec{
		{Module: "github.com/slidebolt/plugin-automation", Dir: "."},
		{Module: "github.com/slidebolt/plugin-test-clean", Dir: "../plugin-test-clean"},
	})
}

// TestMulti_PluginRegisters is the baseline: plugin-automation starts, registers
// its manifest, and exposes its core "groups" device.
func TestMulti_PluginRegisters(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")

	plugins, err := s.Plugins()
	if err != nil {
		t.Fatalf("GET /api/plugins: %v", err)
	}
	reg, ok := plugins["plugin-automation"]
	if !ok {
		t.Fatalf("plugin-automation not in registry")
	}
	if reg.Manifest.ID != "plugin-automation" {
		t.Errorf("manifest ID: got %q, want plugin-automation", reg.Manifest.ID)
	}
	t.Logf("registered: id=%s name=%s version=%s", reg.Manifest.ID, reg.Manifest.Name, reg.Manifest.Version)
}

// TestMulti_CoreDevicePresent asserts the "groups" core device is registered
// on startup — no configuration required.
func TestMulti_CoreDevicePresent(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")

	var devices []map[string]any
	if err := s.GetJSON("/api/plugins/plugin-automation/devices", &devices); err != nil {
		t.Fatalf("GET devices: %v", err)
	}
	found := false
	for _, d := range devices {
		if id, _ := d["id"].(string); id == "groups" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected core device %q, got %d devices", "groups", len(devices))
	}
}

// TestMulti_VirtualGroupCreation seeds two light entities on plugin-test-clean
// with PluginAutomation labels and asserts that plugin-automation creates a
// virtual broadcast Light group entity on the "groups" device.
//
// This exercises the full label→group pipeline without hardware.
func TestMulti_VirtualGroupCreation(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	s.RequirePlugin("plugin-test-clean")

	deviceID := fmt.Sprintf("group-test-device-%d", time.Now().UnixNano())
	groupName := fmt.Sprintf("TestGroup%d", time.Now().UnixNano()%10000)

	seedLight := func(entityID string) {
		entity := types.Entity{
			ID:       entityID,
			DeviceID: deviceID,
			Domain:   "light",
			Labels:   map[string][]string{"PluginAutomation": {groupName}},
		}
		path := fmt.Sprintf("/api/plugins/plugin-test-clean/devices/%s/entities", deviceID)
		if err := s.PostJSON(path, entity, nil); err != nil {
			t.Fatalf("seed entity %s: %v", entityID, err)
		}
	}
	seedLight("light-a")
	seedLight("light-b")
	t.Logf("seeded 2 lights with group label %q", groupName)

	// plugin-automation rebuilds groups reactively (debounced ~500ms).
	expectedID := "group-" + fmt.Sprintf("%s", lowercased(groupName))
	found := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-automation/devices/groups/entities", &entities)
		for _, e := range entities {
			if id, _ := e["id"].(string); id == expectedID {
				return true
			}
		}
		return false
	})
	if !found {
		var entities []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-automation/devices/groups/entities", &entities)
		t.Fatalf("virtual group entity %q not found after 10s; existing entities: %v", expectedID, entityIDs(entities))
	}
	t.Logf("virtual group entity %q created", expectedID)
}

// TestMulti_VirtualLightStripCreation seeds two lights with positional meta
// (index 0 and 1) and asserts plugin-automation creates a virtual light_strip
// entity with strip_members populated correctly.
func TestMulti_VirtualLightStripCreation(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	s.RequirePlugin("plugin-test-clean")

	deviceID := fmt.Sprintf("strip-test-device-%d", time.Now().UnixNano())
	groupName := fmt.Sprintf("TestStrip%d", time.Now().UnixNano()%10000)

	seedStripLight := func(entityID string, index int) {
		metaBlob, _ := json.Marshal(map[string]any{"domain": "light_strip", "index": index})
		entity := types.Entity{
			ID:       entityID,
			DeviceID: deviceID,
			Domain:   "light",
			Labels:   map[string][]string{"PluginAutomation": {groupName}},
			Meta: map[string]json.RawMessage{
				"PluginAutomation:" + groupName: json.RawMessage(metaBlob),
			},
		}
		path := fmt.Sprintf("/api/plugins/plugin-test-clean/devices/%s/entities", deviceID)
		if err := s.PostJSON(path, entity, nil); err != nil {
			t.Fatalf("seed strip light %s: %v", entityID, err)
		}
	}
	seedStripLight("strip-0", 0)
	seedStripLight("strip-1", 1)
	t.Logf("seeded 2 strip lights with positional meta, group=%q", groupName)

	expectedID := "group-" + lowercased(groupName)
	var stripEntity map[string]any
	found := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-automation/devices/groups/entities", &entities)
		for _, e := range entities {
			if id, _ := e["id"].(string); id == expectedID {
				stripEntity = e
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatalf("virtual light_strip entity %q not created after 10s", expectedID)
	}

	// Confirm domain is light_strip, not a plain broadcast group.
	if domain, _ := stripEntity["domain"].(string); domain != "light_strip" {
		t.Errorf("expected domain=light_strip, got %q", domain)
	}
	t.Logf("virtual light_strip entity %q created with domain=%s", expectedID, stripEntity["domain"])
}

// TestMulti_SetSegmentDispatch sends a set_segment command to a virtual
// light_strip and asserts the translated set_rgb arrives on NATS for the
// correct physical entity at index 0.
func TestMulti_SetSegmentDispatch(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	s.RequirePlugin("plugin-test-clean")

	deviceID := fmt.Sprintf("dispatch-device-%d", time.Now().UnixNano())
	groupName := fmt.Sprintf("DispatchStrip%d", time.Now().UnixNano()%10000)

	metaBlob := func(index int) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"domain": "light_strip", "index": index})
		return b
	}
	seedEntity := func(entityID string, index int) {
		entity := types.Entity{
			ID:       entityID,
			DeviceID: deviceID,
			Domain:   "light",
			Labels:   map[string][]string{"PluginAutomation": {groupName}},
			Meta:     map[string]json.RawMessage{"PluginAutomation:" + groupName: metaBlob(index)},
		}
		path := fmt.Sprintf("/api/plugins/plugin-test-clean/devices/%s/entities", deviceID)
		if err := s.PostJSON(path, entity, nil); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seedEntity("seg-light-0", 0)
	seedEntity("seg-light-1", 1)

	// Subscribe to NATS before sending the command.
	nc, err := nats.Connect(s.NATSURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()

	cmdCh := make(chan types.Command, 8)
	sub, _ := nc.Subscribe("slidebolt.rpc.plugin-test-clean.command", func(msg *nats.Msg) {
		var cmd types.Command
		if json.Unmarshal(msg.Data, &cmd) == nil {
			cmdCh <- cmd
		}
	})
	defer sub.Unsubscribe()

	// Wait for the virtual strip entity.
	stripEntityID := "group-" + lowercased(groupName)
	found := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-automation/devices/groups/entities", &entities)
		for _, e := range entities {
			if id, _ := e["id"].(string); id == stripEntityID {
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatalf("strip entity %q not found", stripEntityID)
	}

	// Send set_segment targeting index 0 → should translate to set_rgb on seg-light-0.
	cmdPath := fmt.Sprintf("/api/plugins/plugin-automation/devices/groups/entities/%s/commands", stripEntityID)
	if err := s.PostJSON(cmdPath, map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 0, "rgb": []int{0, 255, 0}},
	}, nil); err != nil {
		t.Fatalf("send set_segment: %v", err)
	}

	select {
	case got := <-cmdCh:
		if got.EntityID != "seg-light-0" {
			t.Errorf("expected entity seg-light-0, got %q", got.EntityID)
		}
		var p struct {
			Type string `json:"type"`
			RGB  []int  `json:"rgb"`
		}
		if json.Unmarshal(got.Payload, &p) == nil {
			if p.Type != "set_rgb" {
				t.Errorf("expected set_rgb, got %q", p.Type)
			}
			t.Logf("dispatched: type=%s rgb=%v entity=%s", p.Type, p.RGB, got.EntityID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for dispatched command on NATS")
	}
}

// --- helpers ---

func lowercased(s string) string {
	out := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		out[i] = c
	}
	return string(out)
}

func entityIDs(entities []map[string]any) []string {
	ids := make([]string, 0, len(entities))
	for _, e := range entities {
		if id, _ := e["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
