package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	automationapp "github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// ==========================================================================
// Bug: Light group entities lose domain.Light state
//
// createLightEntity correctly initialises State as domain.Light{Power: false},
// but two code paths in discoverGroups overwrite it with GroupState:
//
//   1. The control-key path replaces domain.Light with
//      GroupState{…, Control: controlKeys} when the group has control entities.
//
//   2. The inactive-cleanup loop replaces state with
//      GroupState{MemberCount: 0, Status: "inactive"} for any group whose
//      members are not found in the current discovery cycle.
//
// Additionally, handleCommand only logs commands — it never updates the
// group entity's stored state, so the light always appears off.
//
// Because the entity keeps Type "light", plugin-homeassistant unmarshals the
// GroupState JSON {"member_count":0,"status":"inactive"} as domain.Light —
// producing a zero-valued Light (power=false, brightness=0, no colours).
// HA therefore shows every light group as permanently off with no color modes.
// ==========================================================================

// TestLightGroup_ControlKeysMustNotClobberState verifies that when a light
// group member has control metadata, the stored state JSON contains domain.Light
// fields (e.g. "power") rather than GroupState fields (e.g. "member_count").
func TestLightGroup_ControlKeysMustNotClobberState(t *testing.T) {
	e, store, _ := env(t)

	// Member 1: a normal light member.
	saveEntityWithMeta(t, store, "test", "dev1", "bulb0", "light", "Bulb 0",
		domain.Light{Power: true, Brightness: 200},
		map[string][]string{"PluginAutomation": {"Kitchen"}},
		map[string]json.RawMessage{
			"PluginAutomation:Kitchen": json.RawMessage(`{"position":0,"entity":"light"}`),
		})

	// Member 2: also a light member, but with control: true.
	saveEntityWithMeta(t, store, "test", "dev1", "bulb1", "light", "Bulb 1",
		domain.Light{Power: true, Brightness: 180},
		map[string][]string{"PluginAutomation": {"Kitchen"}},
		map[string]json.RawMessage{
			"PluginAutomation:Kitchen": json.RawMessage(`{"position":1,"entity":"light","control":true}`),
		})

	// Start the real plugin-automation App so its discoverGroups runs.
	svc := startAutomationPlugin(t, e)
	_ = svc

	time.Sleep(500 * time.Millisecond)

	// Load the RAW stored bytes to inspect the actual JSON.
	raw := loadGroupRaw(t, store, "kitchen")
	assertLightStateJSON(t, raw, "Kitchen group with control keys")
}

// TestLightGroup_InactiveMustNotClobberState verifies that when a light
// group's members are not found during a discovery cycle, the stored state
// JSON still contains domain.Light fields.
func TestLightGroup_InactiveMustNotClobberState(t *testing.T) {
	e, store, _ := env(t)

	// Pre-seed a light group entity as if it was created by a previous run.
	targetQuery := storage.Query{
		Where: []storage.Filter{{Field: "labels.PluginAutomation", Op: storage.Eq, Value: "Bedroom"}},
	}
	targetJSON, _ := json.Marshal(targetQuery)
	preExisting := domain.Entity{
		ID:       "bedroom",
		Plugin:   automationapp.PluginID,
		DeviceID: "group",
		Type:     "light",
		Name:     "Bedroom",
		Commands: []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_rgb", "light_set_color_temp"},
		State:    domain.Light{Power: true, Brightness: 200},
		Target:   targetJSON,
	}
	if err := store.Save(preExisting); err != nil {
		t.Fatalf("pre-seed group: %v", err)
	}

	// Start the plugin WITHOUT any member entities for "Bedroom".
	svc := startAutomationPlugin(t, e)
	_ = svc

	time.Sleep(500 * time.Millisecond)

	raw := loadGroupRaw(t, store, "bedroom")
	assertLightStateJSON(t, raw, "Bedroom group (inactive, no members)")
}

// TestLightGroup_DiscoveredMembersPreserveState is a sanity check: when
// members are properly discovered (without control metadata), the stored
// state JSON must contain domain.Light fields.
func TestLightGroup_DiscoveredMembersPreserveState(t *testing.T) {
	e, store, _ := env(t)

	for i := 0; i < 3; i++ {
		id := "lamp" + string(rune('a'+i))
		saveEntityWithMeta(t, store, "test", "dev1", id, "light", "Lamp "+id,
			domain.Light{Power: true, Brightness: 200},
			map[string][]string{"PluginAutomation": {"Lounge"}},
			map[string]json.RawMessage{
				"PluginAutomation:Lounge": json.RawMessage(`{"position":` + string(rune('0'+i)) + `,"entity":"light"}`),
			})
	}

	svc := startAutomationPlugin(t, e)
	_ = svc

	time.Sleep(500 * time.Millisecond)

	raw := loadGroupRaw(t, store, "lounge")
	assertLightStateJSON(t, raw, "Lounge group (healthy)")
}

// TestLightGroup_CommandUpdatesState verifies that sending a light_turn_on
// command to a group entity fans out to members, and Watch aggregation
// updates the group state to reflect the member's new state.
func TestLightGroup_CommandUpdatesState(t *testing.T) {
	e, store, cmds := env(t)

	// Seed light members.
	saveEntityWithMeta(t, store, "test", "dev1", "led0", "light", "LED 0",
		domain.Light{Power: false, Brightness: 100},
		map[string][]string{"PluginAutomation": {"Office"}},
		map[string]json.RawMessage{
			"PluginAutomation:Office": json.RawMessage(`{"position":0,"entity":"light"}`),
		})

	// Simulate the member handling its command: when it receives
	// light_turn_on, update its state in storage.
	msg := e.Messenger()
	msg.Subscribe("test.dev1.led0.command.light_turn_on", func(m *messenger.Message) {
		updated := domain.Entity{
			ID: "led0", Plugin: "test", DeviceID: "dev1",
			Type: "light", Name: "LED 0",
			State: domain.Light{Power: true, Brightness: 100},
		}
		store.Save(updated)
	})

	svc := startAutomationPlugin(t, e)
	_ = svc

	time.Sleep(500 * time.Millisecond)

	// The group should exist now.
	groupEntity := domain.Entity{
		Plugin:   automationapp.PluginID,
		DeviceID: "group",
		ID:       "office",
	}

	// Send light_turn_on to the group.
	if err := cmds.Send(groupEntity, domain.LightTurnOn{}); err != nil {
		t.Fatalf("send light_turn_on: %v", err)
	}

	// Give fan-out + member handling + Watch callback time to process.
	time.Sleep(500 * time.Millisecond)

	// Group state should now show power=true from member aggregation.
	assertRawStateField(t, store, "office", "power", true)
}

// TestLightGroup_RediscoveryResetsCommandState verifies that rediscovery
// does NOT overwrite reactive state aggregated from members via Watch.
func TestLightGroup_RediscoveryResetsCommandState(t *testing.T) {
	e, store, cmds := env(t)

	saveEntityWithMeta(t, store, "test", "dev1", "led0", "light", "LED 0",
		domain.Light{Power: false, Brightness: 100},
		map[string][]string{"PluginAutomation": {"Hall"}},
		map[string]json.RawMessage{
			"PluginAutomation:Hall": json.RawMessage(`{"position":0,"entity":"light"}`),
		})

	// Simulate member handling turn_on and set_brightness commands.
	msg := e.Messenger()
	var memberState domain.Light
	memberState.Brightness = 100
	msg.Subscribe("test.dev1.led0.command.light_turn_on", func(m *messenger.Message) {
		memberState.Power = true
		store.Save(domain.Entity{
			ID: "led0", Plugin: "test", DeviceID: "dev1",
			Type: "light", Name: "LED 0",
			State: memberState,
		})
	})
	msg.Subscribe("test.dev1.led0.command.light_set_brightness", func(m *messenger.Message) {
		var cmd domain.LightSetBrightness
		json.Unmarshal(m.Data, &cmd)
		memberState.Brightness = cmd.Brightness
		memberState.Power = true
		store.Save(domain.Entity{
			ID: "led0", Plugin: "test", DeviceID: "dev1",
			Type: "light", Name: "LED 0",
			State: memberState,
		})
	})

	svc := startAutomationPlugin(t, e)
	time.Sleep(500 * time.Millisecond)

	// Send commands to set state via fan-out.
	groupEntity := domain.Entity{Plugin: automationapp.PluginID, DeviceID: "group", ID: "hall"}
	cmds.Send(groupEntity, domain.LightTurnOn{})
	cmds.Send(groupEntity, domain.LightSetBrightness{Brightness: 180})
	time.Sleep(500 * time.Millisecond)

	// Confirm state was aggregated from member.
	assertRawStateField(t, store, "hall", "power", true)
	assertRawStateField(t, store, "hall", "brightness", float64(180))

	// Trigger a real rediscovery cycle via the exported Discover method.
	if err := svc.Discover(); err != nil {
		t.Fatalf("rediscovery: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// State must survive rediscovery.
	assertRawStateField(t, store, "hall", "power", true)
	assertRawStateField(t, store, "hall", "brightness", float64(180))
}

func assertRawStateField(t *testing.T, store storage.Storage, groupID string, field string, expected any) {
	t.Helper()
	raw := loadGroupRaw(t, store, groupID)
	var envelope struct {
		State json.RawMessage `json:"state"`
	}
	json.Unmarshal(raw, &envelope)
	var state map[string]any
	json.Unmarshal(envelope.State, &state)
	if state[field] != expected {
		t.Fatalf("group %q: expected state.%s=%v, got %v (full state: %s)", groupID, field, expected, state[field], string(envelope.State))
	}
}

// -- helpers --

func loadGroupRaw(t *testing.T, store storage.Storage, groupID string) json.RawMessage {
	t.Helper()
	groupKey := domain.EntityKey{Plugin: automationapp.PluginID, DeviceID: "group", ID: groupID}
	raw, err := store.Get(groupKey)
	if err != nil {
		t.Fatalf("group %q not found: %v", groupID, err)
	}
	return raw
}

// assertLightStateJSON checks that the entity's raw JSON contains a "state"
// object with domain.Light fields ("power") rather than GroupState fields
// ("member_count"). This catches the bug where discoverGroups replaces
// domain.Light with GroupState while keeping type="light".
func assertLightStateJSON(t *testing.T, raw json.RawMessage, label string) {
	t.Helper()

	var envelope struct {
		Type  string          `json:"type"`
		State json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("[%s] unmarshal: %v", label, err)
	}
	if envelope.Type != "light" {
		t.Fatalf("[%s] expected type \"light\", got %q", label, envelope.Type)
	}

	stateStr := string(envelope.State)
	if strings.Contains(stateStr, "member_count") {
		t.Fatalf("[%s] state contains GroupState field 'member_count' — domain.Light was clobbered: %s", label, stateStr)
	}
	if strings.Contains(stateStr, `"status"`) {
		t.Fatalf("[%s] state contains GroupState field 'status' — domain.Light was clobbered: %s", label, stateStr)
	}
	if !strings.Contains(stateStr, `"power"`) {
		t.Fatalf("[%s] state missing domain.Light field 'power': %s", label, stateStr)
	}
}

