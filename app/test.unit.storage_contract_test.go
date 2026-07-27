package app

import (
	"encoding/json"
	"reflect"
	"testing"

	domain "github.com/slidebolt/sb-domain"
	testkit "github.com/slidebolt/sb-testkit"
)

func TestStorageContract_GroupEntityRoundTrips(t *testing.T) {
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")

	entity := domain.Entity{
		ID:       "basement",
		Plugin:   PluginID,
		DeviceID: "group",
		Type:     "group",
		Name:     "Basement",
		Commands: []string{"light_turn_on", "script_run", "script_stop_all"},
		State: GroupState{
			MemberCount: 4,
			Status:      "online",
			Control:     []string{"light_turn_on", "light_set_rgb"},
		},
	}
	if err := env.Storage().Save(entity); err != nil {
		t.Fatalf("save entity: %v", err)
	}

	raw, err := env.Storage().Get(domain.EntityKey{Plugin: PluginID, DeviceID: "group", ID: "basement"})
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	var got domain.Entity
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got.Commands, entity.Commands) {
		t.Fatalf("commands = %v, want %v", got.Commands, entity.Commands)
	}
	state, ok := got.State.(GroupState)
	if !ok {
		t.Fatalf("state type = %T", got.State)
	}
	if state.MemberCount != 4 || state.Status != "online" || len(state.Control) != 2 {
		t.Fatalf("state = %+v", state)
	}
}

func TestStorageContract_GroupDiscoveryPreservesProfileFields(t *testing.T) {
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")

	member := domain.Entity{
		ID:       "light1",
		Plugin:   "test",
		DeviceID: "dev1",
		Type:     "light",
		Name:     "Light 1",
		State:    domain.Light{Power: true},
	}
	if err := env.Storage().Save(member); err != nil {
		t.Fatalf("save member: %v", err)
	}
	memberProfile, _ := json.Marshal(map[string]any{
		"labels": map[string][]string{"PluginAutomation": {"IslandLights"}},
	})
	if err := env.Storage().SetProfile(member, json.RawMessage(memberProfile)); err != nil {
		t.Fatalf("set member profile: %v", err)
	}

	groupKey := domain.EntityKey{Plugin: PluginID, DeviceID: "group", ID: "islandlights"}
	groupProfile, _ := json.Marshal(map[string]any{
		"labels": map[string][]string{"PluginHomeassistant": {"true"}},
		"profile": map[string]string{
			"name": "Island Lights",
			"id":   "island_lights",
		},
	})
	if err := env.Storage().SetProfile(groupKey, json.RawMessage(groupProfile)); err != nil {
		t.Fatalf("set group profile: %v", err)
	}

	automation := New()
	if _, err := automation.OnStart(map[string]json.RawMessage{
		"messenger": env.MessengerPayload(),
		"storage":   env.StoragePayload(),
	}); err != nil {
		t.Fatalf("start automation: %v", err)
	}
	t.Cleanup(func() { _ = automation.OnShutdown() })

	raw, err := env.Storage().Get(groupKey)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	var got domain.Entity
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal group: %v", err)
	}
	if got.Profile == nil {
		t.Fatalf("profile missing after discovery: %+v", got)
	}
	if got.Profile.Name != "Island Lights" || got.Profile.ID != "island_lights" {
		t.Fatalf("profile = %+v, want name/id preserved", got.Profile)
	}
	if got.Labels["PluginHomeassistant"][0] != "true" {
		t.Fatalf("PluginHomeassistant label missing: %+v", got.Labels)
	}
	if got.Labels["group_type"][0] != "light" {
		t.Fatalf("group_type label missing: %+v", got.Labels)
	}
}
