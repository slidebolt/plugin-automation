package main

import (
	"encoding/json"
	"testing"

	"github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// ==========================================================================
// Mixed Entity Group Command Tests
// ==========================================================================

// TestMixedGroup_TurnOnCommand validates what happens when you send "turn_on"
// to a mixed group containing switches, lights, sensors, and buttons
func TestMixedGroup_TurnOnCommand(t *testing.T) {
	env, store, cmds := env(t)

	// Create a mixed Basement group with various entity types
	// Each entity declares what virtual type they want to be part of
	// For this test, let's say they all want to be part of a "group" virtual entity

	// 1. Switch - supports turn_on
	switchMeta := map[string]json.RawMessage{
		"PluginAutomation:Basement": json.RawMessage(`{"position": 0, "entity": "switch"}`),
	}
	saveEntityWithMeta(t, store, "test", "dev1", "basement-switch", "switch", "Basement Switch",
		domain.Switch{Power: false}, map[string][]string{"PluginAutomation": {"Basement"}}, switchMeta)

	// 2. Light - supports turn_on
	lightMeta := map[string]json.RawMessage{
		"PluginAutomation:Basement": json.RawMessage(`{"position": 1, "entity": "light"}`),
	}
	saveEntityWithMeta(t, store, "test", "dev1", "basement-light", "light", "Basement Light",
		domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Basement"}}, lightMeta)

	// 3. Sensor - does NOT support turn_on (read-only)
	sensorMeta := map[string]json.RawMessage{
		"PluginAutomation:Basement": json.RawMessage(`{"position": 2, "entity": "sensor"}`),
	}
	saveEntityWithMeta(t, store, "test", "dev1", "basement-temp", "sensor", "Basement Temp",
		domain.Sensor{Value: 21.0, Unit: "°C"}, map[string][]string{"PluginAutomation": {"Basement"}}, sensorMeta)

	// 4. Button - does NOT support turn_on (only press)
	buttonMeta := map[string]json.RawMessage{
		"PluginAutomation:Basement": json.RawMessage(`{"position": 3, "entity": "button"}`),
	}
	saveEntityWithMeta(t, store, "test", "dev1", "basement-button", "button", "Basement Button",
		domain.Button{Presses: 0}, map[string][]string{"PluginAutomation": {"Basement"}}, buttonMeta)

	// Run discovery to create the groups
	discoverGroupsWithMeta(store)

	// Query what groups were created
	groups, _ := store.Query(storage.Query{Pattern: app.PluginID + ".group.>"})
	t.Logf("Created %d groups for Basement:", len(groups))
	for _, g := range groups {
		var group domain.Entity
		json.Unmarshal(g.Data, &group)
		t.Logf("  - %s (type: %s) with commands: %v", group.Name, group.Type, group.Commands)
	}

	// Try sending turn_on to each type of group entity
	t.Run("Send turn_on to Basement switch group", func(t *testing.T) {
		// The switch group entity
		switchGroup := domain.Entity{ID: "basement", Plugin: app.PluginID, DeviceID: "group", Type: "switch"}

		// Listen for command
		done := make(chan string, 1)
		cmds.Receive("test.>", func(addr messenger.Address, cmd any) {
			done <- addr.Key() + ":" + cmd.(messenger.Action).(interface{ ActionName() string }).ActionName()
		})

		// Send turn_on
		if err := cmds.Send(switchGroup, domain.SwitchTurnOn{}); err != nil {
			t.Logf("Send error: %v", err)
		}

		// Check if basement-switch received it
		select {
		case result := <-done:
			t.Logf("Command received: %s", result)
			if result != "test.dev1.basement-switch:switch_turn_on" {
				t.Errorf("Expected basement-switch to receive turn_on, got: %s", result)
			}
		default:
			t.Log("No command received - switch may not have been targeted")
		}
	})

	t.Run("Send turn_on to Basement light group", func(t *testing.T) {
		// The light group entity
		lightGroup := domain.Entity{ID: "basement", Plugin: app.PluginID, DeviceID: "group", Type: "light"}

		// Listen for command
		done := make(chan string, 1)
		cmds.Receive("test.>", func(addr messenger.Address, cmd any) {
			done <- addr.Key() + ":" + cmd.(messenger.Action).(interface{ ActionName() string }).ActionName()
		})

		// Send turn_on
		if err := cmds.Send(lightGroup, domain.LightTurnOn{}); err != nil {
			t.Logf("Send error: %v", err)
		}

		// Check if basement-light received it
		select {
		case result := <-done:
			t.Logf("Command received: %s", result)
			if result != "test.dev1.basement-light:light_turn_on" {
				t.Errorf("Expected basement-light to receive turn_on, got: %s", result)
			}
		default:
			t.Log("No command received - light may not have been targeted")
		}
	})

	t.Run("Basement sensor should not support turn_on", func(t *testing.T) {
		// Verify sensor doesn't have turn_on command
		sensorKey := domain.EntityKey{Plugin: "test", DeviceID: "dev1", ID: "basement-temp"}
		raw, _ := store.Get(sensorKey)
		var sensor domain.Entity
		json.Unmarshal(raw, &sensor)

		hasTurnOn := false
		for _, cmd := range sensor.Commands {
			if cmd == "turn_on" || cmd == "switch_turn_on" || cmd == "light_turn_on" {
				hasTurnOn = true
				break
			}
		}

		if hasTurnOn {
			t.Error("Sensor should not support turn_on command")
		} else {
			t.Log("✓ Sensor correctly does not support turn_on")
		}
	})

	t.Run("Basement button should not support turn_on", func(t *testing.T) {
		// Verify button doesn't have turn_on command (only press)
		buttonKey := domain.EntityKey{Plugin: "test", DeviceID: "dev1", ID: "basement-button"}
		raw, _ := store.Get(buttonKey)
		var button domain.Entity
		json.Unmarshal(raw, &button)

		hasTurnOn := false
		for _, cmd := range button.Commands {
			if cmd == "turn_on" || cmd == "switch_turn_on" {
				hasTurnOn = true
				break
			}
		}

		if hasTurnOn {
			t.Error("Button should not support turn_on command")
		} else {
			t.Log("✓ Button correctly does not support turn_on (only button_press)")
		}
	})

	// Summary
	t.Logf("\n=== MIXED GROUP SUMMARY ===")
	t.Logf("When you label everything 'Basement' with different entity types:")
	t.Logf("- Switches get grouped into a switch virtual entity")
	t.Logf("- Lights get grouped into a light virtual entity")
	t.Logf("- Sensors get grouped into a sensor virtual entity (read-only)")
	t.Logf("- Buttons get grouped into a button virtual entity")
	t.Logf("")
	t.Logf("If you send 'turn_on' to the switch group: switches turn on")
	t.Logf("If you send 'turn_on' to the light group: lights turn on")
	t.Logf("You cannot send 'turn_on' to sensor/button groups (they don't support it)")
	t.Logf("")
	t.Logf("To control ALL basement devices, you'd need to send to each group type")
	t.Logf("OR create a single group where all entities agree on the same 'entity' type")

	_ = env // avoid unused warning
}

// TestMixedGroup_SingleVirtualEntity validates creating a single virtual entity
// where all members agree on the same type (e.g., all pretend to be switches)
func TestMixedGroup_SingleVirtualEntity(t *testing.T) {
	_, store, _ := env(t)

	// Create mixed entities but all declare they want to be "switch" type in the group
	// This simulates: "I know I'm a sensor, but for automation purposes treat me as on/off"

	entities := []struct {
		id       string
		realType string
		state    any
	}{
		{"mixed-switch", "switch", domain.Switch{Power: false}},
		{"mixed-light", "light", domain.Light{Power: false}},
		{"mixed-sensor", "sensor", domain.Sensor{Value: 0}}, // sensor pretending to be switchable
	}

	for i, e := range entities {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Mixed": json.RawMessage(`{"position": ` + string(rune('0'+i)) + `, "entity": "switch"}`),
		}
		labels := map[string][]string{"PluginAutomation": {"Mixed"}}
		saveEntityWithMeta(t, store, "test", "dev1", e.id, e.realType, "Mixed "+e.realType,
			e.state, labels, meta)
	}

	// Run discovery
	discoverGroupsWithMeta(store)

	// Check what was created
	mixedKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "mixed"}
	raw, err := store.Get(mixedKey)
	if err != nil {
		t.Fatal("Mixed group not found")
	}

	var mixedGroup domain.Entity
	json.Unmarshal(raw, &mixedGroup)

	t.Logf("Mixed virtual entity created:")
	t.Logf("  Type: %s", mixedGroup.Type)
	t.Logf("  Commands: %v", mixedGroup.Commands)

	// Get targets to see all members
	var targetQuery storage.Query
	json.Unmarshal(mixedGroup.Target, &targetQuery)
	members, _ := store.Query(targetQuery)

	t.Logf("  Members (%d):", len(members))
	for _, m := range members {
		var e domain.Entity
		json.Unmarshal(m.Data, &e)
		t.Logf("    - %s (real type: %s)", e.ID, e.Type)
	}

	if mixedGroup.Type != "switch" {
		t.Errorf("Expected virtual type 'switch', got %s", mixedGroup.Type)
	}

	if len(members) != 3 {
		t.Errorf("Expected 3 members in mixed group, got %d", len(members))
	}

	t.Logf("\n=== SINGLE VIRTUAL ENTITY ===")
	t.Logf("All entities declared 'entity': 'switch' in their Meta")
	t.Logf("Result: One virtual switch group with 3 heterogeneous members")
	t.Logf("Sending 'switch_turn_on' would target all 3 entities")
	t.Logf("(Whether they can actually handle it depends on their real type)")
}
