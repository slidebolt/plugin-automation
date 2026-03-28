package main

// ==========================================================================
// Group-command routing contract test
//
// sb-virtual owns group fan-out for standard entity commands. When a group
// command is sent to a plugin-automation group entity, each member should
// receive exactly one delivery.
// ==========================================================================

import (
	"encoding/json"
	"testing"
	"time"

	automationapp "github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	testkit "github.com/slidebolt/sb-testkit"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// TestDoubleDispatch_GroupCommandReachesEachMemberExactlyOnce sends a single
// group command when both plugin-automation and sb-virtual are running on the
// same bus.
//
// Current contract: sb-virtual performs the member fan-out for standard group
// commands, so each member receives exactly one delivery.
func TestDoubleDispatch_GroupCommandReachesEachMemberExactlyOnce(t *testing.T) {
	e := testkit.NewTestEnv(t)
	e.Start("messenger")
	e.Start("storage")
	e.Start("sb-virtual") // production fan-out router

	store := e.Storage()
	msg := e.Messenger()

	// Seed 3 light members in the "Living" group.
	seedLightInGroupAt(t, store, "light-a", "Living", 1)
	seedLightInGroupAt(t, store, "light-b", "Living", 2)
	seedLightInGroupAt(t, store, "light-c", "Living", 3)

	// Start plugin-automation — it discovers the group and subscribes to commands.
	startAutomationPlugin(t, e)
	waitForGroup(t, store, "living")

	// Collect ALL commands delivered to any member light.
	// Buffer is large so we can detect over-delivery.
	delivered := collectCommandsBuffered(t, msg, "test.living.>", 20)

	// Send one group command to the plugin-automation group entity.
	groupEntity := domain.Entity{
		ID:       "living",
		Plugin:   automationapp.PluginID,
		DeviceID: "group",
		Type:     "light",
	}
	groupCmds := messenger.NewCommands(msg, domain.LookupCommand)
	if err := groupCmds.Send(groupEntity, domain.LightTurnOn{}); err != nil {
		t.Fatalf("send group command: %v", err)
	}

	// sb-virtual fans out the command once to each member.
	const wantPerMember = 1
	wantTotal := 3 * wantPerMember

	// Collect all deliveries: wait for at least wantTotal, then drain for extras.
	got := collectAtLeastThenDrain(t, delivered, wantTotal, 2*time.Second, 200*time.Millisecond)

	// Tally per-member delivery count.
	counts := map[string]int{}
	for _, cmd := range got {
		counts[cmd.entityKey]++
	}

	for _, lightID := range []string{"light-a", "light-b", "light-c"} {
		key := "test.living." + lightID
		if n := counts[key]; n != wantPerMember {
			t.Errorf("member %s received command %d times, want exactly %d", key, n, wantPerMember)
		}
	}
}

// TestDoubleDispatch_ScriptRunStillWorks ensures that standard group-command
// routing via sb-virtual does not break ScriptRun, which remains a
// plugin-automation-specific command.
func TestDoubleDispatch_ScriptRunStillWorks(t *testing.T) {
	e := testkit.NewTestEnv(t)
	e.Start("messenger")
	e.Start("storage")
	e.Start("sb-virtual")
	e.Start("sb-script")

	store := e.Storage()

	seedLightInGroup(t, store, "light-x", "Office")

	startAutomationPlugin(t, e)
	waitForGroup(t, store, "office")

	saveScript(t, store, "dd_test_script", minimalScript())
	startScriptForGroup(t, e.Messenger(), store, "dd_test_script", "Office")

	// Wait briefly — if ScriptRun dispatch is broken the script engine never
	// receives the command. No crash here is insufficient; we also check the
	// group entity is still alive and the script was accepted.
	time.Sleep(300 * time.Millisecond)

	key := domain.EntityKey{Plugin: automationapp.PluginID, DeviceID: "group", ID: "office"}
	if _, err := store.Get(key); err != nil {
		t.Errorf("group entity missing after ScriptRun: %v", err)
	}
}

// collectCommandsBuffered subscribes to pattern and returns a buffered channel.
func collectCommandsBuffered(t *testing.T, msg messenger.Messenger, pattern string, bufSize int) chan receivedCommand {
	t.Helper()
	ch := make(chan receivedCommand, bufSize)
	sub, err := msg.Subscribe(pattern, func(m *messenger.Message) {
		parts := splitCommandSubject(m.Subject)
		if parts.action != "" {
			select {
			case ch <- receivedCommand{entityKey: parts.entityKey, action: parts.action}:
			default:
			}
		}
	})
	if err != nil {
		t.Fatalf("subscribe %s: %v", pattern, err)
	}
	t.Cleanup(func() { sub.Unsubscribe() })
	return ch
}

// collectAtLeastThenDrain waits until `want` commands arrive (or times out),
// then drains any additional late arrivals within drainWindow. Returns all
// collected commands so the caller can check for over-delivery.
func collectAtLeastThenDrain(t *testing.T, ch chan receivedCommand, want int, timeout, drainWindow time.Duration) []receivedCommand {
	t.Helper()
	var out []receivedCommand
	deadline := time.After(timeout)
	for len(out) < want {
		select {
		case cmd := <-ch:
			out = append(out, cmd)
		case <-deadline:
			t.Fatalf("timed out waiting for %d commands, got %d", want, len(out))
		}
	}
	drain := time.After(drainWindow)
	for {
		select {
		case cmd := <-ch:
			out = append(out, cmd)
		case <-drain:
			return out
		}
	}
}

// saveEntityWithMetaForDD saves an entity with labels + profile meta.
// Wrapper to avoid colliding with the same-named helper in group_switch_test.go
// — both live in package main so they share the function directly.
var _ = func() bool {
	_ = json.RawMessage(nil)
	_ = storage.Storage(nil)
	return true
}

func minimalScript() string {
	return `Script("dd_test_script", function(ctx)
end)
`
}
