package main

import (
	"encoding/json"
	"testing"
	"time"

	automationapp "github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
)

// ==========================================================================
// Tests for group light state aggregation from member states.
//
// Groups should reflect the actual state of their members:
//   - power: true if ANY member is on
//   - brightness: average of on-members
//   - rgb: average of on-members with rgb set
//   - temperature: average of on-members with temperature set
// ==========================================================================

// --- Unit tests for the pure aggregation function ---

func TestAggregateLightState_MixedOnOff(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 200},
		{Power: true, Brightness: 100},
		{Power: false, Brightness: 0},
	}
	result := automationapp.AggregateLightState(members)

	if !result.Power {
		t.Fatal("expected power=true when some members are on")
	}
	// Average of on-members: (200+100)/2 = 150
	if result.Brightness != 150 {
		t.Fatalf("expected brightness=150, got %d", result.Brightness)
	}
}

func TestAggregateLightState_AllOff(t *testing.T) {
	members := []domain.Light{
		{Power: false, Brightness: 0},
		{Power: false, Brightness: 0},
	}
	result := automationapp.AggregateLightState(members)

	if result.Power {
		t.Fatal("expected power=false when all members are off")
	}
	if result.Brightness != 0 {
		t.Fatalf("expected brightness=0, got %d", result.Brightness)
	}
}

func TestAggregateLightState_AllOn(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 255},
		{Power: true, Brightness: 200},
		{Power: true, Brightness: 155},
	}
	result := automationapp.AggregateLightState(members)

	if !result.Power {
		t.Fatal("expected power=true")
	}
	// (255+200+155)/3 = 203
	if result.Brightness != 203 {
		t.Fatalf("expected brightness=203, got %d", result.Brightness)
	}
}

func TestAggregateLightState_RGB(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 200, RGB: []int{255, 0, 0}},
		{Power: true, Brightness: 200, RGB: []int{0, 0, 255}},
		{Power: false, Brightness: 0, RGB: []int{0, 255, 0}}, // off — excluded
	}
	result := automationapp.AggregateLightState(members)

	if !result.Power {
		t.Fatal("expected power=true")
	}
	// Average RGB of on-members with RGB: (255+0)/2=127, (0+0)/2=0, (0+255)/2=127
	if len(result.RGB) != 3 {
		t.Fatalf("expected rgb len 3, got %d", len(result.RGB))
	}
	if result.RGB[0] != 127 || result.RGB[1] != 0 || result.RGB[2] != 127 {
		t.Fatalf("expected rgb=[127,0,127], got %v", result.RGB)
	}
}

func TestAggregateLightState_Temperature(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 200, Temperature: 300},
		{Power: true, Brightness: 200, Temperature: 400},
		{Power: true, Brightness: 100}, // on but no temperature
	}
	result := automationapp.AggregateLightState(members)

	// Average temperature of on-members that have temperature: (300+400)/2 = 350
	if result.Temperature != 350 {
		t.Fatalf("expected temperature=350, got %d", result.Temperature)
	}
}

func TestAggregateLightState_Empty(t *testing.T) {
	result := automationapp.AggregateLightState(nil)
	if result.Power {
		t.Fatal("expected power=false for empty members")
	}
	if result.Brightness != 0 {
		t.Fatalf("expected brightness=0, got %d", result.Brightness)
	}
}

func TestLightGroup_ColorTempOnlyCommandsOmitRGB(t *testing.T) {
	e, store, _ := env(t)

	for i, id := range []string{"back01", "center", "back02"} {
		meta := mustMarshalRaw(t, map[string]any{"position": i, "entity": "light"})
		e := saveEntityWithMeta(t, store, "plugin-esphome", "movieroom", id, "light", "Bulb",
			domain.Light{Power: false, Brightness: 88, ColorMode: "cold_warm_white", Temperature: 370, White: 255},
			map[string][]string{"PluginAutomation": {"BasementMovieRoomEdison"}},
			map[string]json.RawMessage{"PluginAutomation:BasementMovieRoomEdison": meta},
		)
		e.Commands = []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp", "light_set_rgb"}
		if err := store.Save(e); err != nil {
			t.Fatalf("resave %s: %v", id, err)
		}
	}

	svc := startAutomationPlugin(t, e)
	_ = svc
	time.Sleep(500 * time.Millisecond)

	raw := loadGroupRaw(t, store, "basementmovieroomedison")
	var got struct {
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal group: %v", err)
	}
	want := []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp", "script_run", "script_stop_all"}
	if len(got.Commands) != len(want) {
		t.Fatalf("commands = %v, want %v", got.Commands, want)
	}
	for i := range want {
		if got.Commands[i] != want[i] {
			t.Fatalf("commands = %v, want %v", got.Commands, want)
		}
	}
}

// --- Integration test: group aggregates from real member entities ---

func TestLightGroup_AggregatesFromMemberState(t *testing.T) {
	e, store, _ := env(t)

	// Seed 3 lights: 2 on at different brightness, 1 off.
	saveEntityWithMeta(t, store, "plugin-wiz", "dev1", "bulb0", "light", "Bulb 0",
		domain.Light{Power: true, Brightness: 200, RGB: []int{255, 0, 0}},
		map[string][]string{"PluginAutomation": {"Dining"}},
		map[string]json.RawMessage{
			"PluginAutomation:Dining": json.RawMessage(`{"position":0,"entity":"light"}`),
		})
	saveEntityWithMeta(t, store, "plugin-wiz", "dev1", "bulb1", "light", "Bulb 1",
		domain.Light{Power: true, Brightness: 100, RGB: []int{0, 0, 255}},
		map[string][]string{"PluginAutomation": {"Dining"}},
		map[string]json.RawMessage{
			"PluginAutomation:Dining": json.RawMessage(`{"position":1,"entity":"light"}`),
		})
	saveEntityWithMeta(t, store, "plugin-wiz", "dev1", "bulb2", "light", "Bulb 2",
		domain.Light{Power: false, Brightness: 0},
		map[string][]string{"PluginAutomation": {"Dining"}},
		map[string]json.RawMessage{
			"PluginAutomation:Dining": json.RawMessage(`{"position":2,"entity":"light"}`),
		})

	svc := startAutomationPlugin(t, e)
	_ = svc
	time.Sleep(500 * time.Millisecond)

	// The group should reflect the aggregate of its members.
	raw := loadGroupRaw(t, store, "dining")
	var envelope struct {
		State json.RawMessage `json:"state"`
	}
	json.Unmarshal(raw, &envelope)
	var state map[string]any
	json.Unmarshal(envelope.State, &state)

	// power = any-on → true
	if state["power"] != true {
		t.Fatalf("expected power=true (2 of 3 on), got %v (state: %s)", state["power"], string(envelope.State))
	}

	// brightness = avg of on-members: (200+100)/2 = 150
	if state["brightness"] != float64(150) {
		t.Fatalf("expected brightness=150, got %v (state: %s)", state["brightness"], string(envelope.State))
	}
}

func TestLightGroup_ReactsToMemberStateChange(t *testing.T) {
	e, store, _ := env(t)

	// Seed 2 lights, both off.
	saveEntityWithMeta(t, store, "plugin-wiz", "dev1", "led0", "light", "LED 0",
		domain.Light{Power: false, Brightness: 0},
		map[string][]string{"PluginAutomation": {"Office"}},
		map[string]json.RawMessage{
			"PluginAutomation:Office": json.RawMessage(`{"position":0,"entity":"light"}`),
		})
	saveEntityWithMeta(t, store, "plugin-wiz", "dev1", "led1", "light", "LED 1",
		domain.Light{Power: false, Brightness: 0},
		map[string][]string{"PluginAutomation": {"Office"}},
		map[string]json.RawMessage{
			"PluginAutomation:Office": json.RawMessage(`{"position":1,"entity":"light"}`),
		})

	svc := startAutomationPlugin(t, e)
	_ = svc
	time.Sleep(500 * time.Millisecond)

	// Group should be off initially.
	assertRawStateField(t, store, "office", "power", false)

	// Simulate an external state change: led0 turns on (e.g. via HA or Wiz app).
	// Save the updated entity to storage — this triggers state.changed event.
	updatedLED := domain.Entity{
		ID: "led0", Plugin: "plugin-wiz", DeviceID: "dev1",
		Type: "light", Name: "LED 0",
		State:  domain.Light{Power: true, Brightness: 200},
		Labels: map[string][]string{"PluginAutomation": {"Office"}},
	}
	store.Save(updatedLED)

	// Give the Watch callback time to fire and aggregate.
	time.Sleep(300 * time.Millisecond)

	// Group should now reflect the on member.
	assertRawStateField(t, store, "office", "power", true)
	assertRawStateField(t, store, "office", "brightness", float64(200))
}

func TestLightGroup_AllMembersOff(t *testing.T) {
	e, store, _ := env(t)

	saveEntityWithMeta(t, store, "plugin-wiz", "dev1", "dim0", "light", "Dim 0",
		domain.Light{Power: false, Brightness: 0},
		map[string][]string{"PluginAutomation": {"Bedroom"}},
		map[string]json.RawMessage{
			"PluginAutomation:Bedroom": json.RawMessage(`{"position":0,"entity":"light"}`),
		})
	saveEntityWithMeta(t, store, "plugin-wiz", "dev1", "dim1", "light", "Dim 1",
		domain.Light{Power: false, Brightness: 0},
		map[string][]string{"PluginAutomation": {"Bedroom"}},
		map[string]json.RawMessage{
			"PluginAutomation:Bedroom": json.RawMessage(`{"position":1,"entity":"light"}`),
		})

	svc := startAutomationPlugin(t, e)
	_ = svc
	time.Sleep(500 * time.Millisecond)

	raw := loadGroupRaw(t, store, "bedroom")
	var envelope struct {
		State json.RawMessage `json:"state"`
	}
	json.Unmarshal(raw, &envelope)
	var state map[string]any
	json.Unmarshal(envelope.State, &state)

	if state["power"] != false {
		t.Fatalf("expected power=false when all off, got %v", state["power"])
	}
	if state["brightness"] != float64(0) {
		t.Fatalf("expected brightness=0, got %v", state["brightness"])
	}
}
