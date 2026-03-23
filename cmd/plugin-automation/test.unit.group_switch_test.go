package main

import (
	"encoding/json"
	"testing"
	"time"

	automationapp "github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
	managersdk "github.com/slidebolt/sb-manager-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	scriptserver "github.com/slidebolt/sb-script/server"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// ==========================================================================
// Group Control Tests
//
// When a light group's metadata includes a "control" key pointing to an
// entity (switch, binary_sensor, button, etc.), plugin-automation should:
//   1. Subscribe to that entity's state changes
//   2. Stop any running scripts targeting the group's members
//   3. Forward on/off to the group's light members
//
// The user experience is: label a control entity and lights with the same
// group, set "control": "plugin.device.entity_id" in the meta, done.
// ==========================================================================

// TestGroupSwitch_TurnOnLights validates that when a switch turns on,
// all lights in the group receive light_turn_on.
func TestGroupSwitch_TurnOnLights(t *testing.T) {
	e, store, msg := groupSwitchEnv(t)

	seedGroupSwitchEntities(t, store, "Kitchen", "test.kitchen.switch001", []string{
		"kitchen-light-1",
		"kitchen-light-2",
		"kitchen-light-3",
	})

	startAutomationPlugin(t, e)
	waitForGroup(t, store, "kitchen")

	received := collectCommands(t, msg, "test.kitchen.>", 3)

	publishSwitchState(t, msg, "test.kitchen.switch001", true)

	cmds := waitForCommands(t, received, 3, time.Second)
	for _, cmd := range cmds {
		if cmd.action != "light_turn_on" {
			t.Errorf("expected light_turn_on, got %s for %s", cmd.action, cmd.entityKey)
		}
	}
}

// TestGroupSwitch_TurnOffLights validates that when a switch turns off,
// all lights in the group receive light_turn_off.
func TestGroupSwitch_TurnOffLights(t *testing.T) {
	e, store, msg := groupSwitchEnv(t)

	seedGroupSwitchEntities(t, store, "Kitchen", "test.kitchen.switch001", []string{
		"kitchen-light-1",
		"kitchen-light-2",
	})

	startAutomationPlugin(t, e)
	waitForGroup(t, store, "kitchen")

	received := collectCommands(t, msg, "test.kitchen.>", 2)

	publishSwitchState(t, msg, "test.kitchen.switch001", false)

	cmds := waitForCommands(t, received, 2, time.Second)
	for _, cmd := range cmds {
		if cmd.action != "light_turn_off" {
			t.Errorf("expected light_turn_off, got %s for %s", cmd.action, cmd.entityKey)
		}
	}
}

// TestGroupSwitch_StopsRunningScriptsFirst validates that when the switch
// fires, any running scripts targeting the group's members are stopped
// before forwarding the on/off command.
func TestGroupSwitch_StopsRunningScriptsFirst(t *testing.T) {
	e, store, msg := groupSwitchEnv(t)

	seedGroupSwitchEntities(t, store, "Kitchen", "test.kitchen.switch001", []string{
		"kitchen-light-1",
		"kitchen-light-2",
	})

	// Start plugin-automation first so groups are discovered.
	startAutomationPlugin(t, e)
	waitForGroup(t, store, "kitchen")

	// Start sb-script server.
	startScriptServer(t, e, store)

	// Save and start a script targeting the kitchen group.
	saveScript(t, store, "party_time", partyTimeSource())
	startScriptForGroup(t, msg, store, "party_time", "Kitchen")

	// Verify the script is running.
	kitchenGroup := getEntity(t, store, automationapp.PluginID, "group", "kitchen")
	kitchenQueryRef := mustGroupQueryRef(t, kitchenGroup)
	hash := hashScriptInstance("party_time", kitchenQueryRef)
	if err := waitForScriptInstance(store, hash, 500*time.Millisecond); err != nil {
		t.Fatalf("expected party_time script to be running: %v", err)
	}

	// Switch turns on — this should stop the script first.
	publishSwitchState(t, msg, "test.kitchen.switch001", true)

	if err := waitForScriptStopped(store, hash, time.Second); err != nil {
		t.Fatalf("expected switch activation to stop running script: %v", err)
	}
}

// TestGroupSwitch_OnlySendsToLightsInGroup validates that when the switch
// fires, only lights in the same group receive commands — lights in other
// groups are unaffected.
func TestGroupSwitch_OnlySendsToLightsInGroup(t *testing.T) {
	e, store, msg := groupSwitchEnv(t)

	seedGroupSwitchEntities(t, store, "Kitchen", "test.kitchen.switch001", []string{
		"kitchen-light-1",
	})
	seedLightInGroup(t, store, "bedroom-light-1", "Bedroom")

	startAutomationPlugin(t, e)
	waitForGroup(t, store, "kitchen")

	kitchenCmds := collectCommands(t, msg, "test.kitchen.>", 1)
	bedroomCmds := collectCommands(t, msg, "test.bedroom.>", 1)

	publishSwitchState(t, msg, "test.kitchen.switch001", true)

	cmds := waitForCommands(t, kitchenCmds, 1, time.Second)
	if len(cmds) != 1 || cmds[0].action != "light_turn_on" {
		t.Errorf("expected 1 light_turn_on for kitchen, got %v", cmds)
	}

	select {
	case cmd := <-bedroomCmds:
		t.Errorf("bedroom should not receive commands, got %s for %s", cmd.action, cmd.entityKey)
	case <-time.After(200 * time.Millisecond):
		// Good — no command received.
	}
}

// TestGroupControl_BinarySensorTurnsOnLights validates that a binary_sensor
// (e.g. motion sensor) can control lights via the same "control" mechanism.
func TestGroupControl_BinarySensorTurnsOnLights(t *testing.T) {
	e, store, msg := groupSwitchEnv(t)

	seedGroupControlEntities(t, store, "Hallway", "zigbee.hallway.motion001", "binary_sensor", []string{
		"hallway-light-1",
		"hallway-light-2",
	})

	startAutomationPlugin(t, e)
	waitForGroup(t, store, "hallway")

	received := collectCommands(t, msg, "test.hallway.>", 2)

	// Motion sensor activates.
	publishBinarySensorState(t, msg, "zigbee.hallway.motion001", true)

	cmds := waitForCommands(t, received, 2, time.Second)
	for _, cmd := range cmds {
		if cmd.action != "light_turn_on" {
			t.Errorf("expected light_turn_on, got %s for %s", cmd.action, cmd.entityKey)
		}
	}
}

// TestGroupControl_BinarySensorTurnsOffLights validates that a binary_sensor
// going off turns lights off.
func TestGroupControl_BinarySensorTurnsOffLights(t *testing.T) {
	e, store, msg := groupSwitchEnv(t)

	seedGroupControlEntities(t, store, "Hallway", "zigbee.hallway.motion001", "binary_sensor", []string{
		"hallway-light-1",
	})

	startAutomationPlugin(t, e)
	waitForGroup(t, store, "hallway")

	received := collectCommands(t, msg, "test.hallway.>", 1)

	publishBinarySensorState(t, msg, "zigbee.hallway.motion001", false)

	cmds := waitForCommands(t, received, 1, time.Second)
	if cmds[0].action != "light_turn_off" {
		t.Errorf("expected light_turn_off, got %s", cmds[0].action)
	}
}

// TestGroupControl_MultipleSwitches validates that two switches can both
// control the same group — either one firing turns lights on/off.
func TestGroupControl_MultipleSwitches(t *testing.T) {
	e, store, msg := groupSwitchEnv(t)

	// Seed two switches with "control": true and 2 lights, all in Garage group.
	seedSwitchInGroup(t, store, "test.garage.front-switch", "Garage", 0)
	seedSwitchInGroup(t, store, "test.garage.back-switch", "Garage", 1)
	seedLightInGroup(t, store, "garage-light-1", "Garage")
	seedLightInGroupAt(t, store, "garage-light-2", "Garage", 3)

	startAutomationPlugin(t, e)
	waitForGroup(t, store, "garage")

	// Front switch turns on — both lights should receive light_turn_on.
	received := collectCommands(t, msg, "test.garage.>", 4)
	publishSwitchState(t, msg, "test.garage.front-switch", true)

	cmds := waitForCommands(t, received, 2, time.Second)
	for _, cmd := range cmds {
		if cmd.action != "light_turn_on" {
			t.Errorf("front switch on: expected light_turn_on, got %s for %s", cmd.action, cmd.entityKey)
		}
	}

	// Back switch turns off — both lights should receive light_turn_off.
	publishSwitchState(t, msg, "test.garage.back-switch", false)

	cmds = waitForCommands(t, received, 2, time.Second)
	for _, cmd := range cmds {
		if cmd.action != "light_turn_off" {
			t.Errorf("back switch off: expected light_turn_off, got %s for %s", cmd.action, cmd.entityKey)
		}
	}
}

// TestGroupSwitch_NoSwitchConfigured validates that groups without a
// "control" key in their meta do not react to state changes.
func TestGroupSwitch_NoSwitchConfigured(t *testing.T) {
	e, store, msg := groupSwitchEnv(t)

	lightMeta := map[string]json.RawMessage{
		"PluginAutomation:Hallway": json.RawMessage(`{"position": 1, "entity": "light"}`),
	}
	saveEntityWithMeta(t, store, "test", "hallway", "hallway-light-1", "light", "Hallway Light 1",
		domain.Light{Power: false},
		map[string][]string{"PluginAutomation": {"Hallway"}},
		lightMeta)

	startAutomationPlugin(t, e)
	waitForGroup(t, store, "hallway")

	hallwayCmds := collectCommands(t, msg, "test.hallway.>", 1)
	publishSwitchState(t, msg, "test.hallway.some-switch", true)

	select {
	case cmd := <-hallwayCmds:
		t.Errorf("group without switch config should not forward commands, got %s", cmd.action)
	case <-time.After(200 * time.Millisecond):
		// Good.
	}
}

// ==========================================================================
// Helpers
// ==========================================================================

type receivedCommand struct {
	entityKey string
	action    string
}

func groupSwitchEnv(t *testing.T) (*managersdk.TestEnv, storage.Storage, messenger.Messenger) {
	t.Helper()
	e := managersdk.NewTestEnv(t)
	e.Start("messenger")
	e.Start("storage")
	return e, e.Storage(), e.Messenger()
}

func startAutomationPlugin(t *testing.T, e *managersdk.TestEnv) *automationapp.App {
	t.Helper()
	svc := automationapp.New()
	_, err := svc.OnStart(map[string]json.RawMessage{
		"messenger": e.MessengerPayload(),
		"storage":   nil,
	})
	if err != nil {
		t.Fatalf("start plugin-automation: %v", err)
	}
	t.Cleanup(func() { _ = svc.OnShutdown() })
	return svc
}

func startScriptServer(t *testing.T, e *managersdk.TestEnv, store storage.Storage) *scriptserver.Service {
	t.Helper()
	scriptMsg, err := messenger.Connect(map[string]json.RawMessage{
		"messenger": e.MessengerPayload(),
	})
	if err != nil {
		t.Fatalf("script messenger: %v", err)
	}
	svc, err := scriptserver.New(scriptMsg, store)
	if err != nil {
		t.Fatalf("start sb-script: %v", err)
	}
	if err := scriptMsg.Flush(); err != nil {
		t.Fatalf("flush script subs: %v", err)
	}
	t.Cleanup(func() { svc.Shutdown(); scriptMsg.Close() })
	return svc
}

func seedGroupSwitchEntities(t *testing.T, store storage.Storage, groupName, switchKey string, lightIDs []string) {
	t.Helper()

	// Seed the switch entity with "control": true — it declares itself as the controller.
	parts := splitEntityKey(switchKey)
	saveEntityWithMeta(t, store, parts.plugin, parts.device, parts.id, "switch", groupName+" Switch",
		domain.Switch{Power: false},
		map[string][]string{"PluginAutomation": {groupName}},
		map[string]json.RawMessage{
			"PluginAutomation:" + groupName: mustMarshalRaw(t, map[string]any{
				"position": 0,
				"entity":   "switch",
				"control":  true,
			}),
		})

	// Seed light entities — no control field needed, just group membership.
	for i, lightID := range lightIDs {
		device := automationapp.NormalizeGroupID(groupName)
		saveEntityWithMeta(t, store, "test", device, lightID, "light", lightID,
			domain.Light{Power: false},
			map[string][]string{"PluginAutomation": {groupName}},
			map[string]json.RawMessage{
				"PluginAutomation:" + groupName: mustMarshalRaw(t, map[string]any{
					"position": i + 1,
					"entity":   "light",
				}),
			})
	}
}

func seedLightInGroup(t *testing.T, store storage.Storage, lightID, groupName string) {
	t.Helper()
	device := automationapp.NormalizeGroupID(groupName)
	saveEntityWithMeta(t, store, "test", device, lightID, "light", lightID,
		domain.Light{Power: false},
		map[string][]string{"PluginAutomation": {groupName}},
		map[string]json.RawMessage{
			"PluginAutomation:" + groupName: mustMarshalRaw(t, map[string]any{
				"position": 1,
				"entity":   "light",
			}),
		})
}

func seedSwitchInGroup(t *testing.T, store storage.Storage, switchKey, groupName string, position int) {
	t.Helper()
	parts := splitEntityKey(switchKey)
	saveEntityWithMeta(t, store, parts.plugin, parts.device, parts.id, "switch", groupName+" Switch",
		domain.Switch{Power: false},
		map[string][]string{"PluginAutomation": {groupName}},
		map[string]json.RawMessage{
			"PluginAutomation:" + groupName: mustMarshalRaw(t, map[string]any{
				"position": position,
				"entity":   "switch",
				"control":  true,
			}),
		})
}

func seedLightInGroupAt(t *testing.T, store storage.Storage, lightID, groupName string, position int) {
	t.Helper()
	device := automationapp.NormalizeGroupID(groupName)
	saveEntityWithMeta(t, store, "test", device, lightID, "light", lightID,
		domain.Light{Power: false},
		map[string][]string{"PluginAutomation": {groupName}},
		map[string]json.RawMessage{
			"PluginAutomation:" + groupName: mustMarshalRaw(t, map[string]any{
				"position": position,
				"entity":   "light",
			}),
		})
}

func publishSwitchState(t *testing.T, msg messenger.Messenger, switchKey string, power bool) {
	t.Helper()
	parts := splitEntityKey(switchKey)
	ent := domain.Entity{
		ID:       parts.id,
		Plugin:   parts.plugin,
		DeviceID: parts.device,
		Type:     "switch",
		State:    domain.Switch{Power: power},
	}
	data, err := json.Marshal(ent)
	if err != nil {
		t.Fatalf("marshal switch entity: %v", err)
	}
	if err := msg.Publish("state.changed."+switchKey, data); err != nil {
		t.Fatalf("publish switch state: %v", err)
	}
}

func collectCommands(t *testing.T, msg messenger.Messenger, pattern string, bufSize int) chan receivedCommand {
	t.Helper()
	ch := make(chan receivedCommand, bufSize+10)
	sub, err := msg.Subscribe(pattern, func(m *messenger.Message) {
		subj := m.Subject
		parts := splitCommandSubject(subj)
		if parts.action != "" {
			ch <- receivedCommand{entityKey: parts.entityKey, action: parts.action}
		}
	})
	if err != nil {
		t.Fatalf("subscribe %s: %v", pattern, err)
	}
	t.Cleanup(func() { sub.Unsubscribe() })
	return ch
}

func waitForCommands(t *testing.T, ch chan receivedCommand, count int, timeout time.Duration) []receivedCommand {
	t.Helper()
	var cmds []receivedCommand
	deadline := time.After(timeout)
	for len(cmds) < count {
		select {
		case cmd := <-ch:
			cmds = append(cmds, cmd)
		case <-deadline:
			t.Fatalf("timed out waiting for %d commands, got %d: %v", count, len(cmds), cmds)
		}
	}
	return cmds
}

func waitForGroup(t *testing.T, store storage.Storage, groupID string) {
	t.Helper()
	key := domain.EntityKey{Plugin: automationapp.PluginID, DeviceID: "group", ID: groupID}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := store.Get(key); err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for group %s", groupID)
}

func saveScript(t *testing.T, store storage.Storage, name, source string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"type":     "script",
		"language": "lua",
		"name":     name,
		"source":   source,
	})
	if err != nil {
		t.Fatalf("marshal script %s: %v", name, err)
	}
	if err := store.Save(integrationScriptDef{key: "sb-script.scripts." + name, data: body}); err != nil {
		t.Fatalf("save script %s: %v", name, err)
	}
}

type integrationScriptDef struct {
	key  string
	data json.RawMessage
}

func (b integrationScriptDef) Key() string                  { return b.key }
func (b integrationScriptDef) MarshalJSON() ([]byte, error) { return b.data, nil }

func startScriptForGroup(t *testing.T, msg messenger.Messenger, store storage.Storage, name, groupName string) {
	t.Helper()
	groupID := automationapp.NormalizeGroupID(groupName)
	group := getEntity(t, store, automationapp.PluginID, "group", groupID)
	queryRef := mustGroupQueryRef(t, group)
	saveGroupQueryRef(t, store, group, queryRef)
	resp := integrationScriptAPI(t, msg, "script.start", map[string]any{
		"name":     name,
		"queryRef": queryRef,
	})
	if !resp.OK {
		t.Fatalf("start script %s: %s", name, resp.Error)
	}
}

func partyTimeSource() string {
	return `Automation("party_time", {
  trigger = Interval({min=0.05, max=0.1}),
  targets = None(),
}, function(ctx)
  ctx.targets:each(function(e)
    if e.type == "light" then
      ctx.send(e, "light_set_rgb", {r=255, g=0, b=255})
    end
  end)
end)
`
}

type entityKeyParts struct {
	plugin string
	device string
	id     string
}

func splitEntityKey(key string) entityKeyParts {
	parts := [3]string{}
	idx := 0
	start := 0
	for i, r := range key {
		if r == '.' && idx < 2 {
			parts[idx] = key[start:i]
			idx++
			start = i + 1
		}
	}
	parts[idx] = key[start:]
	return entityKeyParts{plugin: parts[0], device: parts[1], id: parts[2]}
}

type commandSubjectParts struct {
	entityKey string
	action    string
}

func splitCommandSubject(subj string) commandSubjectParts {
	const marker = ".command."
	idx := -1
	for i := 0; i <= len(subj)-len(marker); i++ {
		if subj[i:i+len(marker)] == marker {
			idx = i
			break
		}
	}
	if idx < 0 {
		return commandSubjectParts{}
	}
	return commandSubjectParts{
		entityKey: subj[:idx],
		action:    subj[idx+len(marker):],
	}
}

// seedGroupControlEntities seeds a control entity of any type + light members.
func seedGroupControlEntities(t *testing.T, store storage.Storage, groupName, controlKey, controlType string, lightIDs []string) {
	t.Helper()

	parts := splitEntityKey(controlKey)
	var controlState any
	switch controlType {
	case "switch":
		controlState = domain.Switch{Power: false}
	case "binary_sensor":
		controlState = domain.BinarySensor{On: false}
	case "button":
		controlState = domain.Button{Presses: 0}
	}

	// Control entity declares "control": true on itself.
	saveEntityWithMeta(t, store, parts.plugin, parts.device, parts.id, controlType, groupName+" Control",
		controlState,
		map[string][]string{"PluginAutomation": {groupName}},
		map[string]json.RawMessage{
			"PluginAutomation:" + groupName: mustMarshalRaw(t, map[string]any{
				"position": 0,
				"entity":   controlType,
				"control":  true,
			}),
		})

	for i, lightID := range lightIDs {
		device := automationapp.NormalizeGroupID(groupName)
		saveEntityWithMeta(t, store, "test", device, lightID, "light", lightID,
			domain.Light{Power: false},
			map[string][]string{"PluginAutomation": {groupName}},
			map[string]json.RawMessage{
				"PluginAutomation:" + groupName: mustMarshalRaw(t, map[string]any{
					"position": i + 1,
					"entity":   "light",
				}),
			})
	}
}

func publishBinarySensorState(t *testing.T, msg messenger.Messenger, key string, on bool) {
	t.Helper()
	parts := splitEntityKey(key)
	ent := domain.Entity{
		ID:       parts.id,
		Plugin:   parts.plugin,
		DeviceID: parts.device,
		Type:     "binary_sensor",
		State:    domain.BinarySensor{On: on},
	}
	data, err := json.Marshal(ent)
	if err != nil {
		t.Fatalf("marshal binary_sensor entity: %v", err)
	}
	if err := msg.Publish("state.changed."+key, data); err != nil {
		t.Fatalf("publish binary_sensor state: %v", err)
	}
}

func mustMarshalRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
