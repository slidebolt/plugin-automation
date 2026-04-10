package app

import (
	"context"
	"encoding/json"
	"testing"

	domain "github.com/slidebolt/sb-domain"
	logcfg "github.com/slidebolt/sb-logging"
	logging "github.com/slidebolt/sb-logging-sdk"
	"github.com/slidebolt/sb-logging/server"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	testkit "github.com/slidebolt/sb-testkit"
)

func TestHandleCommandAppendsCommandLog(t *testing.T) {
	svc, err := server.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("server.New(memory): %v", err)
	}
	logger := svc.Store()
	app := NewWithLogger(logger)
	addr := messenger.Address{
		Plugin:   PluginID,
		DeviceID: "group",
		EntityID: "basement",
	}

	app.handleCommand(addr, domain.LightTurnOn{})

	events, err := logger.List(context.Background(), logging.ListRequest{
		Kind:   "command.received",
		Entity: addr.Key(),
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("List len: got %d want 1", len(events))
	}
	if events[0].Action != "light_turn_on" {
		t.Fatalf("Action: got %q want %q", events[0].Action, "light_turn_on")
	}
	if events[0].Source != PluginID {
		t.Fatalf("Source: got %q want %q", events[0].Source, PluginID)
	}
}

func TestHandleCommandWithTraceAppendsTraceID(t *testing.T) {
	svc, err := server.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("server.New(memory): %v", err)
	}
	logger := svc.Store()
	app := NewWithLogger(logger)
	addr := messenger.Address{
		Plugin:   PluginID,
		DeviceID: "group",
		EntityID: "basement",
	}

	app.handleCommandWithTrace(addr, domain.LightTurnOff{}, "trace-basement-1")

	events, err := logger.List(context.Background(), logging.ListRequest{
		Kind:    "command.received",
		Entity:  addr.Key(),
		TraceID: "trace-basement-1",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("List len: got %d want 1", len(events))
	}
	if events[0].TraceID != "trace-basement-1" {
		t.Fatalf("TraceID: got %q want %q", events[0].TraceID, "trace-basement-1")
	}
}

func TestHandleCommandAppendsScriptRunFailureLog(t *testing.T) {
	svc, err := server.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("server.New(memory): %v", err)
	}
	logger := svc.Store()
	app := NewWithLogger(logger)
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	app.msg = env.Messenger()
	app.store = env.Storage()
	addr := messenger.Address{
		Plugin:   PluginID,
		DeviceID: "group",
		EntityID: "basement",
	}
	groupTarget, err := json.Marshal(map[string]any{
		"pattern": "",
		"where": []map[string]any{
			{"field": "labels.PluginAutomation", "op": "eq", "value": "Basement"},
		},
	})
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	if err := app.store.Save(domain.Entity{
		ID:       "basement",
		Plugin:   PluginID,
		DeviceID: "group",
		Type:     "light",
		Name:     "Basement",
		Target:   groupTarget,
		State:    domain.Light{Power: false},
	}); err != nil {
		t.Fatalf("save group: %v", err)
	}

	app.handleCommand(addr, ScriptRun{Name: "PartyTime"})

	events, err := logger.List(context.Background(), logging.ListRequest{
		Kind:   "script.run.failed",
		Entity: addr.Key(),
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("List len: got %d want 1", len(events))
	}
	if events[0].Action != "script_run" {
		t.Fatalf("Action: got %q want %q", events[0].Action, "script_run")
	}
	if events[0].Data["script_name"] != "PartyTime" {
		t.Fatalf("script_name: got %v want %q", events[0].Data["script_name"], "PartyTime")
	}
}
