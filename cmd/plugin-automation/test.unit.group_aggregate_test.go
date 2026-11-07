package main

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	automationapp "github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
)

// ==========================================================================
// Tests for group light state aggregation from member states.
//
// Groups should reflect the actual state of their members:
//   - power: true if ANY member is on
//   - brightness: average of on-members
//   - color: only when all on-members agree within a small reporting tolerance
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

func TestAggregateLightState_RGBConsensus(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 200, RGB: []int{255, 100, 20}},
		{Power: true, Brightness: 200, RGB: []int{247, 108, 24}},
		{Power: false, Brightness: 0, RGB: []int{0, 255, 0}}, // off — excluded
	}
	result := automationapp.AggregateLightState(members)

	if !result.Power {
		t.Fatal("expected power=true")
	}
	if len(result.RGB) != 3 {
		t.Fatalf("expected rgb len 3, got %d", len(result.RGB))
	}
	if result.RGB[0] != 251 || result.RGB[1] != 104 || result.RGB[2] != 22 {
		t.Fatalf("expected rgb=[251,104,22], got %v", result.RGB)
	}
}

func TestAggregateLightState_RGBDisagreementOmitsColor(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 200, RGB: []int{255, 0, 0}},
		{Power: true, Brightness: 200, RGB: []int{0, 0, 255}},
	}
	result := automationapp.AggregateLightState(members)

	if len(result.RGB) != 0 {
		t.Fatalf("expected no rgb for mismatched members, got %v", result.RGB)
	}
	if result.ColorMode != "" {
		t.Fatalf("expected empty colorMode for mismatched members, got %q", result.ColorMode)
	}
}

func TestAggregateLightState_MixedRGBAndWarmWhiteOmitsRGB(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 127, ColorMode: "rgb", RGB: []int{255, 0, 0}, Temperature: 370},
		{Power: true, Brightness: 127, ColorMode: "rgb", RGB: []int{0, 0, 255}, Temperature: 370},
		{Power: true, Brightness: 127, ColorMode: "cold_warm_white", Temperature: 370},
	}
	result := automationapp.AggregateLightState(members)

	if len(result.RGB) != 0 {
		t.Fatalf("expected no rgb for mixed party/warm-white state, got %v", result.RGB)
	}
	if result.ColorMode != "color_temp" {
		t.Fatalf("expected colorMode=color_temp from matching temperatures, got %q", result.ColorMode)
	}
	if result.Temperature != 370 {
		t.Fatalf("expected temperature=370, got %d", result.Temperature)
	}
}

func TestAggregateLightState_XYConsensus(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 200, ColorMode: "xy", XY: []float64{0.20, 0.30}},
		{Power: true, Brightness: 200, ColorMode: "xy", XY: []float64{0.204, 0.304}},
		{Power: false, Brightness: 0, ColorMode: "xy", XY: []float64{0.80, 0.90}}, // off, excluded
	}
	result := automationapp.AggregateLightState(members)

	if result.ColorMode != "xy" {
		t.Fatalf("expected colorMode=xy, got %q", result.ColorMode)
	}
	if len(result.XY) != 2 {
		t.Fatalf("expected xy len 2, got %d", len(result.XY))
	}
	if math.Abs(result.XY[0]-0.202) > 0.000001 || math.Abs(result.XY[1]-0.302) > 0.000001 {
		t.Fatalf("expected xy=[0.202,0.302], got %v", result.XY)
	}
}

func TestAggregateLightState_HSConsensus(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 200, ColorMode: "hs", HS: []float64{20, 30}},
		{Power: true, Brightness: 200, ColorMode: "hs", HS: []float64{20.4, 30.4}},
	}
	result := automationapp.AggregateLightState(members)

	if result.ColorMode != "hs" {
		t.Fatalf("expected colorMode=hs, got %q", result.ColorMode)
	}
	if len(result.HS) != 2 {
		t.Fatalf("expected hs len 2, got %d", len(result.HS))
	}
	if math.Abs(result.HS[0]-20.2) > 0.000001 || math.Abs(result.HS[1]-30.2) > 0.000001 {
		t.Fatalf("expected hs=[20.2,30.2], got %v", result.HS)
	}
}

func TestAggregateLightState_TemperatureConsensus(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 200, Temperature: 366},
		{Power: true, Brightness: 200, Temperature: 374},
		{Power: true, Brightness: 100}, // on but no temperature
	}
	result := automationapp.AggregateLightState(members)

	if result.Temperature != 0 {
		t.Fatalf("expected no temperature when an on member lacks temperature, got %d", result.Temperature)
	}
	if result.ColorMode != "" {
		t.Fatalf("expected empty colorMode when an on member lacks temperature, got %q", result.ColorMode)
	}
}

func TestAggregateLightState_AllTemperatureMembersWithinThreshold(t *testing.T) {
	members := []domain.Light{
		{Power: true, Brightness: 200, Temperature: 366},
		{Power: true, Brightness: 200, Temperature: 374},
		{Power: false, Brightness: 100},
	}
	result := automationapp.AggregateLightState(members)

	if result.Temperature != 370 {
		t.Fatalf("expected temperature=370, got %d", result.Temperature)
	}
	if result.ColorMode != "color_temp" {
		t.Fatalf("expected colorMode=color_temp, got %q", result.ColorMode)
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

func TestLightStatesEqual(t *testing.T) {
	a := domain.Light{
		Power:       true,
		Brightness:  127,
		ColorMode:   "rgb",
		RGB:         []int{10, 20, 30},
		RGBW:        []int{10, 20, 30, 40},
		RGBWW:       []int{10, 20, 30, 40, 50},
		HS:          []float64{120, 60},
		XY:          []float64{0.3126, 0.3291},
		Temperature: 370,
		White:       255,
		Effect:      "none",
	}
	b := a
	b.RGB = append([]int(nil), a.RGB...)
	b.RGBW = append([]int(nil), a.RGBW...)
	b.RGBWW = append([]int(nil), a.RGBWW...)
	b.HS = append([]float64(nil), a.HS...)
	b.XY = append([]float64(nil), a.XY...)

	if !automationapp.LightStatesEqual(a, b) {
		t.Fatal("expected equal light states")
	}

	b.RGB[2] = 31
	if automationapp.LightStatesEqual(a, b) {
		t.Fatal("expected changed RGB to differ")
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
		e.Commands = []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp"}
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

func TestLightGroup_MixedColorMembersAdvertiseColorCommands(t *testing.T) {
	e, store, _ := env(t)

	edisonMeta := mustMarshalRaw(t, map[string]any{"position": 0, "entity": "light"})
	edison := saveEntityWithMeta(t, store, "plugin-esphome", "basement", "edison", "light", "Edison",
		domain.Light{Power: false, Brightness: 88, ColorMode: "cold_warm_white", Temperature: 370, White: 255},
		map[string][]string{"PluginAutomation": {"Basement"}},
		map[string]json.RawMessage{"PluginAutomation:Basement": edisonMeta},
	)
	edison.Commands = []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp"}
	if err := store.Save(edison); err != nil {
		t.Fatalf("resave edison: %v", err)
	}

	colorMeta := mustMarshalRaw(t, map[string]any{"position": 1, "entity": "light"})
	color := saveEntityWithMeta(t, store, "plugin-wiz", "basement", "color", "light", "Color",
		domain.Light{Power: false, Brightness: 88},
		map[string][]string{"PluginAutomation": {"Basement"}},
		map[string]json.RawMessage{"PluginAutomation:Basement": colorMeta},
	)
	color.Commands = []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp", "light_set_hs"}
	if err := store.Save(color); err != nil {
		t.Fatalf("resave color: %v", err)
	}

	svc := startAutomationPlugin(t, e)
	_ = svc
	time.Sleep(500 * time.Millisecond)

	raw := loadGroupRaw(t, store, "basement")
	var got struct {
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal group: %v", err)
	}
	want := []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp", "light_set_rgb", "light_set_hs", "script_run", "script_stop_all"}
	if len(got.Commands) != len(want) {
		t.Fatalf("commands = %v, want %v", got.Commands, want)
	}
	for i := range want {
		if got.Commands[i] != want[i] {
			t.Fatalf("commands = %v, want %v", got.Commands, want)
		}
	}
}

func TestLightGroup_ColorStateWithoutCommandsDoesNotAdvertiseColor(t *testing.T) {
	e, store, _ := env(t)

	meta := mustMarshalRaw(t, map[string]any{"position": 0, "entity": "light"})
	member := saveEntityWithMeta(t, store, "plugin-wiz", "dining", "color", "light", "Color",
		domain.Light{Power: false, Brightness: 71, ColorMode: "rgb", RGB: []int{233, 2, 255}},
		map[string][]string{"PluginAutomation": {"DiningLights"}},
		map[string]json.RawMessage{"PluginAutomation:DiningLights": meta},
	)
	member.Commands = []string{"light_turn_on", "light_turn_off", "light_set_brightness"}
	if err := store.Save(member); err != nil {
		t.Fatalf("resave member: %v", err)
	}

	svc := startAutomationPlugin(t, e)
	_ = svc
	time.Sleep(500 * time.Millisecond)

	raw := loadGroupRaw(t, store, "dininglights")
	var got struct {
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal group: %v", err)
	}
	want := []string{"light_turn_on", "light_turn_off", "light_set_brightness", "script_run", "script_stop_all"}
	if len(got.Commands) != len(want) {
		t.Fatalf("commands = %v, want %v", got.Commands, want)
	}
	for i := range want {
		if got.Commands[i] != want[i] {
			t.Fatalf("commands = %v, want %v", got.Commands, want)
		}
	}
}

func TestLightGroup_XYMembersAdvertiseXYCommand(t *testing.T) {
	e, store, _ := env(t)

	for i, id := range []string{"left", "right"} {
		meta := mustMarshalRaw(t, map[string]any{"position": i, "entity": "light"})
		light := saveEntityWithMeta(t, store, "plugin-zigbee", "alex", id, "light", "Alex Light",
			domain.Light{Power: true, Brightness: 88, ColorMode: "xy", XY: []float64{0.3126, 0.2103}},
			map[string][]string{"PluginAutomation": {"AlexRoom"}},
			map[string]json.RawMessage{"PluginAutomation:AlexRoom": meta},
		)
		light.Commands = []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp", "light_set_rgb", "light_set_hs", "light_set_xy"}
		if err := store.Save(light); err != nil {
			t.Fatalf("resave %s: %v", id, err)
		}
	}

	svc := startAutomationPlugin(t, e)
	_ = svc
	time.Sleep(500 * time.Millisecond)

	raw := loadGroupRaw(t, store, "alexroom")
	var got struct {
		Commands []string        `json:"commands"`
		State    json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal group: %v", err)
	}
	if !containsString(got.Commands, "light_set_xy") {
		t.Fatalf("commands = %v, want light_set_xy", got.Commands)
	}
	var state domain.Light
	if err := json.Unmarshal(got.State, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if state.ColorMode != "xy" || len(state.XY) != 2 {
		t.Fatalf("state = %+v, want xy color state", state)
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

func TestLightGroup_SkipsUnchangedAggregateSave(t *testing.T) {
	e, store, _ := env(t)
	msg := e.Messenger()

	member := saveEntityWithMeta(t, store, "plugin-wiz", "dev1", "bulb0", "light", "Bulb 0",
		domain.Light{Power: true, Brightness: 100, RGB: []int{10, 20, 30}, ColorMode: "rgb"},
		map[string][]string{"PluginAutomation": {"Dining"}},
		map[string]json.RawMessage{
			"PluginAutomation:Dining": json.RawMessage(`{"position":0,"entity":"light"}`),
		})

	svc := startAutomationPlugin(t, e)
	_ = svc
	time.Sleep(500 * time.Millisecond)

	groupEvents := make(chan struct{}, 4)
	sub, err := msg.Subscribe("state.changed.plugin-automation.group.dining", func(_ *messenger.Message) {
		groupEvents <- struct{}{}
	})
	if err != nil {
		t.Fatalf("subscribe group changes: %v", err)
	}
	defer sub.Unsubscribe()
	if err := msg.Flush(); err != nil {
		t.Fatalf("flush subscribe: %v", err)
	}

	if err := store.Save(member); err != nil {
		t.Fatalf("resave unchanged member: %v", err)
	}
	select {
	case <-groupEvents:
		t.Fatal("unexpected group publish for unchanged aggregate")
	case <-time.After(200 * time.Millisecond):
	}

	member.State = domain.Light{Power: true, Brightness: 101, RGB: []int{10, 20, 30}, ColorMode: "rgb"}
	if err := store.Save(member); err != nil {
		t.Fatalf("resave changed member: %v", err)
	}
	select {
	case <-groupEvents:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for changed aggregate publish")
	}
}

func TestLightGroup_SkipsUnchangedRediscoverySave(t *testing.T) {
	e, store, _ := env(t)
	msg := e.Messenger()

	saveEntityWithMeta(t, store, "plugin-wiz", "dev1", "bulb0", "light", "Bulb 0",
		domain.Light{Power: true, Brightness: 100, RGB: []int{10, 20, 30}, ColorMode: "rgb"},
		map[string][]string{"PluginAutomation": {"Dining"}},
		map[string]json.RawMessage{
			"PluginAutomation:Dining": json.RawMessage(`{"position":0,"entity":"light"}`),
		})

	svc := startAutomationPlugin(t, e)
	time.Sleep(500 * time.Millisecond)

	groupEvents := make(chan struct{}, 2)
	sub, err := msg.Subscribe("state.changed.plugin-automation.group.dining", func(_ *messenger.Message) {
		groupEvents <- struct{}{}
	})
	if err != nil {
		t.Fatalf("subscribe group changes: %v", err)
	}
	defer sub.Unsubscribe()
	if err := msg.Flush(); err != nil {
		t.Fatalf("flush subscribe: %v", err)
	}
drainStartup:
	for {
		select {
		case <-groupEvents:
		case <-time.After(200 * time.Millisecond):
			break drainStartup
		}
	}

	if err := svc.Discover(); err != nil {
		t.Fatalf("rediscovery: %v", err)
	}
	select {
	case <-groupEvents:
		t.Fatal("unexpected group publish for unchanged rediscovery")
	case <-time.After(200 * time.Millisecond):
	}
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
