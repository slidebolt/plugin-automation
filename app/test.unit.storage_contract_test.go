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
