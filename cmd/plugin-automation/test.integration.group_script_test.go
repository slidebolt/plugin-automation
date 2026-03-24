package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	automationapp "github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
	testkit "github.com/slidebolt/sb-testkit"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	scriptserver "github.com/slidebolt/sb-script/server"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type integrationScriptResp struct {
	OK    bool   `json:"ok"`
	Hash  string `json:"hash,omitempty"`
	Error string `json:"error,omitempty"`
}

func TestGroup_ScriptRun_StartsScriptForGroup(t *testing.T) {
	e, store, _, cmds := groupScriptEnv(t)

	_ = e
	basement := getEntity(t, store, automationapp.PluginID, "group", "basement")
	if err := cmds.Send(basement, automationapp.ScriptRun{Name: "party_time"}); err != nil {
		t.Fatalf("send script_run: %v", err)
	}

	basementQueryRef := mustGroupQueryRef(t, basement)
	hash := hashScriptInstance("party_time", basementQueryRef)

	if err := waitForScriptInstance(store, hash, 500*time.Millisecond); err != nil {
		t.Fatalf("expected script_run to start party_time for Basement group: %v", err)
	}
}

func TestGroup_ScriptRun_StopsOverlappingHallwayScriptFirst(t *testing.T) {
	_, store, msg, cmds := groupScriptEnv(t)

	hallway := getEntity(t, store, automationapp.PluginID, "group", "hallway")
	hallwayQueryRef := mustGroupQueryRef(t, hallway)
	saveGroupQueryRef(t, store, hallway, hallwayQueryRef)
	startResp := integrationScriptAPI(t, msg, "script.start", map[string]any{
		"name":     "party_time",
		"queryRef": hallwayQueryRef,
	})
	if !startResp.OK {
		t.Fatalf("script.start hallway: %s", startResp.Error)
	}

	hallwayHash := hashScriptInstance("party_time", hallwayQueryRef)
	if err := waitForScriptInstance(store, hallwayHash, 500*time.Millisecond); err != nil {
		t.Fatalf("expected initial hallway script to be running: %v", err)
	}

	basement := getEntity(t, store, automationapp.PluginID, "group", "basement")
	if err := cmds.Send(basement, automationapp.ScriptRun{Name: "red_color"}); err != nil {
		t.Fatalf("send basement script_run: %v", err)
	}

	if err := waitForScriptStopped(store, hallwayHash, 500*time.Millisecond); err != nil {
		t.Fatalf("expected Basement script_run to stop overlapping Hallway script first: %v", err)
	}

	basementHash := hashScriptInstance("red_color", mustGroupQueryRef(t, basement))
	if err := waitForScriptInstance(store, basementHash, 500*time.Millisecond); err != nil {
		t.Fatalf("expected Basement script_run to start red_color: %v", err)
	}
}

func TestGroup_ScriptStopAll_StopsOverlappingHallwayScript(t *testing.T) {
	_, store, msg, cmds := groupScriptEnv(t)

	hallway := getEntity(t, store, automationapp.PluginID, "group", "hallway")
	hallwayQueryRef := mustGroupQueryRef(t, hallway)
	saveGroupQueryRef(t, store, hallway, hallwayQueryRef)
	startResp := integrationScriptAPI(t, msg, "script.start", map[string]any{
		"name":     "party_time",
		"queryRef": hallwayQueryRef,
	})
	if !startResp.OK {
		t.Fatalf("script.start hallway: %s", startResp.Error)
	}

	hallwayHash := hashScriptInstance("party_time", hallwayQueryRef)
	if err := waitForScriptInstance(store, hallwayHash, 500*time.Millisecond); err != nil {
		t.Fatalf("expected initial hallway script to be running: %v", err)
	}

	basement := getEntity(t, store, automationapp.PluginID, "group", "basement")
	if err := cmds.Send(basement, automationapp.ScriptStopAll{}); err != nil {
		t.Fatalf("send script_stop_all: %v", err)
	}

	if err := waitForScriptStopped(store, hallwayHash, 500*time.Millisecond); err != nil {
		t.Fatalf("expected Basement script_stop_all to stop overlapping Hallway script: %v", err)
	}
}

func groupScriptEnv(t *testing.T) (*testkit.TestEnv, storage.Storage, messenger.Messenger, *messenger.Commands) {
	t.Helper()

	e := testkit.NewTestEnv(t)
	e.Start("messenger")
	e.Start("storage")

	store := e.Storage()
	msg := e.Messenger()
	cmds := messenger.NewCommands(msg, domain.LookupCommand)

	seedAutomationLight(t, store, "basement-office-1", "Office 1", []groupSpec{
		{name: "Basement", position: 1},
		{name: "Office", position: 1},
	})
	seedAutomationLight(t, store, "basement-hallway-1", "Basement Hallway 1", []groupSpec{
		{name: "Basement", position: 2},
		{name: "Hallway", position: 1},
	})
	seedAutomationLight(t, store, "basement-hallway-2", "Basement Hallway 2", []groupSpec{
		{name: "Basement", position: 3},
		{name: "Hallway", position: 2},
	})
	seedAutomationLight(t, store, "upstairs-hallway-1", "Upstairs Hallway 1", []groupSpec{
		{name: "Hallway", position: 3},
	})
	seedAutomationLight(t, store, "upstairs-hallway-2", "Upstairs Hallway 2", []groupSpec{
		{name: "Hallway", position: 4},
	})

	automationSvc := automationapp.New()
	_, err := automationSvc.OnStart(map[string]json.RawMessage{
		"messenger": e.MessengerPayload(),
		"storage":   nil,
	})
	if err != nil {
		t.Fatalf("start plugin-automation: %v", err)
	}
	t.Cleanup(func() { _ = automationSvc.OnShutdown() })

	for _, groupID := range []string{"basement", "hallway", "office"} {
		if err := waitForEntity(t, store, domain.EntityKey{
			Plugin:   automationapp.PluginID,
			DeviceID: "group",
			ID:       groupID,
		}, time.Second); err != nil {
			t.Fatal(err)
		}
	}

	scriptMsg, err := messenger.Connect(map[string]json.RawMessage{"messenger": e.MessengerPayload()})
	if err != nil {
		t.Fatalf("sb-script messenger: %v", err)
	}
	scriptSvc, err := scriptserver.New(scriptMsg, store)
	if err != nil {
		t.Fatalf("start sb-script: %v", err)
	}
	if err := scriptMsg.Flush(); err != nil {
		t.Fatalf("flush sb-script subscriptions: %v", err)
	}
	t.Cleanup(func() { scriptSvc.Shutdown(); scriptMsg.Close() })

	for _, def := range []string{"party_time", "red_color"} {
		body, err := json.Marshal(map[string]string{
			"type":     "script",
			"language": "lua",
			"name":     def,
			"source":   colorScriptSource(def),
		})
		if err != nil {
			t.Fatalf("marshal %s: %v", def, err)
		}
		if err := store.Save(scriptDefBlob{key: "sb-script.scripts." + def, data: body}); err != nil {
			t.Fatalf("save script %s: %v", def, err)
		}
	}

	return e, store, msg, cmds
}

type groupSpec struct {
	name     string
	position int
}

type scriptInstanceKey struct {
	Hash string
}

type scriptDefBlob struct {
	key  string
	data json.RawMessage
}

func (k scriptInstanceKey) Key() string {
	return fmt.Sprintf("sb-script.instances.%s", k.Hash)
}

func (b scriptDefBlob) Key() string                  { return b.key }
func (b scriptDefBlob) MarshalJSON() ([]byte, error) { return b.data, nil }

func hashScriptInstance(name, query string) string {
	const offset = uint64(14695981039346656037)
	const prime = uint64(1099511628211)
	h := offset
	for _, b := range []byte(name + "\x00" + query) {
		h ^= uint64(b)
		h *= prime
	}
	return fmt.Sprintf("%016x", h)
}

func seedAutomationLight(t *testing.T, store storage.Storage, id, name string, groups []groupSpec) {
	t.Helper()

	labels := map[string][]string{}
	meta := map[string]json.RawMessage{}
	for _, group := range groups {
		labels["PluginAutomation"] = append(labels["PluginAutomation"], group.name)
		raw, err := json.Marshal(map[string]any{
			"position": group.position,
			"entity":   "light",
		})
		if err != nil {
			t.Fatalf("marshal group meta %s/%s: %v", id, group.name, err)
		}
		meta["PluginAutomation:"+group.name] = raw
	}

	entity := domain.Entity{
		ID:       id,
		Plugin:   "test",
		DeviceID: id,
		Type:     "light",
		Name:     name,
		Commands: []string{"light_turn_on", "light_turn_off", "light_set_rgb"},
		State: domain.Light{
			Power:     true,
			ColorMode: "rgb",
		},
	}
	if err := store.Save(entity); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}

	profile := make(map[string]any)
	if len(labels) > 0 {
		profile["labels"] = labels
	}
	if len(meta) > 0 {
		profile["meta"] = meta
	}
	if len(profile) > 0 {
		data, _ := json.Marshal(profile)
		if err := store.SetProfile(entity, json.RawMessage(data)); err != nil {
			t.Fatalf("setprofile %s: %v", id, err)
		}
	}
}

func waitForEntity(t *testing.T, store storage.Storage, key domain.EntityKey, timeout time.Duration) error {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := store.Get(key); err == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return &timeoutErr{what: key.Key()}
}

func waitForScriptInstance(store storage.Storage, hash string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := store.Get(scriptInstanceKey{Hash: hash}); err == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return &timeoutErr{what: scriptInstanceKey{Hash: hash}.Key()}
}

func waitForScriptStopped(store storage.Storage, hash string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := store.Get(scriptInstanceKey{Hash: hash}); err != nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return &timeoutErr{what: "stopped " + scriptInstanceKey{Hash: hash}.Key()}
}

func mustGroupQueryRef(t *testing.T, group domain.Entity) string {
	t.Helper()
	return fmt.Sprintf("plugin_automation_group_%s_%s", group.DeviceID, group.ID)
}

func saveGroupQueryRef(t *testing.T, store storage.Storage, group domain.Entity, ref string) {
	t.Helper()
	var q storage.Query
	if err := json.Unmarshal(group.Target, &q); err != nil {
		t.Fatalf("unmarshal group target for %s: %v", group.Key(), err)
	}
	if err := storage.EnsureQueryLayout(store); err != nil {
		t.Fatalf("ensure query layout: %v", err)
	}
	if err := storage.SaveQueryDefinition(store, ref, q); err != nil {
		t.Fatalf("save query definition %s: %v", ref, err)
	}
}

func integrationScriptAPI(t *testing.T, msg messenger.Messenger, subject string, body any) integrationScriptResp {
	t.Helper()

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal %s: %v", subject, err)
	}
	resp, err := msg.Request(subject, data, 5*time.Second)
	if err != nil {
		t.Fatalf("request %s: %v", subject, err)
	}

	var out integrationScriptResp
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		t.Fatalf("parse %s response: %v", subject, err)
	}
	return out
}

type timeoutErr struct {
	what string
}

func (e *timeoutErr) Error() string {
	return "timed out waiting for " + e.what
}

func colorScriptSource(name string) string {
	return fmt.Sprintf(`Automation(%q, {
  trigger = Interval({min=0.05, max=0.1}),
  targets = None(),
}, function(ctx)
  ctx.targets:each(function(e)
    if e.state.colorMode == "rgb" then
      ctx.send(e, "light_set_rgb", {r=255, g=0, b=255})
    end
  end)
end)
`, name)
}
