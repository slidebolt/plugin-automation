package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/slidebolt/sdk-types"
)

// fakeCommandService captures SendCommand calls for assertions.
type fakeCommandService struct {
	sent []types.Command
	err  error // if non-nil, returned from SendCommand
}

func (f *fakeCommandService) SendCommand(cmd types.Command) error {
	f.sent = append(f.sent, cmd)
	return f.err
}

// newTestPluginWithCommands returns a plugin wired with a fakeCommandService.
func newTestPluginWithCommands(t *testing.T) (*PluginAutomationPlugin, *fakeCommandService) {
	t.Helper()
	p, _ := newTestPlugin(t)
	svc := &fakeCommandService{}
	p.pctx.Commands = svc
	return p, svc
}

// makeStripEntity builds a minimal light_strip entity with the given strip members.
func makeStripEntity(members []stripMember) types.Entity {
	raw, err := json.Marshal(members)
	if err != nil {
		panic(err)
	}
	return types.Entity{
		ID:     "group-basement",
		Domain: "light_strip",
		Meta:   map[string]json.RawMessage{"strip_members": raw},
	}
}

func makeCommand(payload any) types.Command {
	b, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return types.Command{ID: "cmd-1", Payload: b}
}

// ── OnCommand routing ──────────────────────────────────────────────────────

// OnCommand for a light_strip entity routes to handleStripCommand.
// A valid set_segment command must succeed and dispatch a downstream command.
func TestOnCommand_LightStrip_SetSegment_Dispatches(t *testing.T) {
	p, svc := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "plugin-esphome", DeviceID: "bridge", EntityID: "led-0"},
		{Index: 1, PluginID: "plugin-esphome", DeviceID: "bridge", EntityID: "led-1"},
	})
	cmd := makeCommand(map[string]any{
		"type": "set_segment",
		"segment": map[string]any{"index": 1, "rgb": []int{255, 128, 0}},
	})

	if err := p.OnCommand(cmd, entity); err != nil {
		t.Fatalf("OnCommand: %v", err)
	}
	if len(svc.sent) != 1 {
		t.Fatalf("expected 1 downstream command, got %d", len(svc.sent))
	}
	sent := svc.sent[0]
	if sent.EntityID != "led-1" {
		t.Errorf("downstream EntityID: got %q, want %q", sent.EntityID, "led-1")
	}
	var payload map[string]any
	_ = json.Unmarshal(sent.Payload, &payload)
	if payload["type"] != "set_rgb" {
		t.Errorf("downstream type: got %v, want set_rgb", payload["type"])
	}
}

// OnCommand for an unknown domain returns nil but should log an error.
// We can't capture the log here, but we verify it does NOT error or panic.
func TestOnCommand_UnknownDomain_ReturnsNil(t *testing.T) {
	p, svc := newTestPluginWithCommands(t)

	entity := types.Entity{ID: "some-entity", Domain: "light"} // broadcast group, unexpected here
	cmd := makeCommand(map[string]any{"type": "turn_on"})

	if err := p.OnCommand(cmd, entity); err != nil {
		t.Fatalf("expected nil for unknown domain, got: %v", err)
	}
	if len(svc.sent) != 0 {
		t.Errorf("expected no downstream commands for unknown domain, got %d", len(svc.sent))
	}
}

// ── handleStripCommand ──────────────────────────────────────────────────────

// set_segment with RGB dispatches set_rgb to the correct member.
func TestHandleStripCommand_SetSegment_RGB(t *testing.T) {
	p, svc := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "plug-a", DeviceID: "dev-a", EntityID: "led-0"},
		{Index: 1, PluginID: "plug-b", DeviceID: "dev-b", EntityID: "led-1"},
		{Index: 2, PluginID: "plug-a", DeviceID: "dev-a", EntityID: "led-2"},
	})
	cmd := makeCommand(map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 2, "rgb": []int{10, 20, 30}},
	})

	if err := p.OnCommand(cmd, entity); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svc.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(svc.sent))
	}
	sent := svc.sent[0]
	if sent.PluginID != "plug-a" || sent.DeviceID != "dev-a" || sent.EntityID != "led-2" {
		t.Errorf("wrong target: %+v", sent)
	}
	var payload map[string]any
	_ = json.Unmarshal(sent.Payload, &payload)
	if payload["type"] != "set_rgb" {
		t.Errorf("downstream type: got %v, want set_rgb", payload["type"])
	}
	rgb, _ := payload["rgb"].([]any)
	if len(rgb) != 3 || rgb[0].(float64) != 10 || rgb[1].(float64) != 20 || rgb[2].(float64) != 30 {
		t.Errorf("downstream rgb: %v", payload["rgb"])
	}
}

// set_segment with brightness dispatches set_brightness to the correct member.
func TestHandleStripCommand_SetSegment_Brightness(t *testing.T) {
	p, svc := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "plug-a", DeviceID: "dev-a", EntityID: "led-0"},
	})
	brightness := 128
	cmd := makeCommand(map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 0, "brightness": brightness},
	})

	if err := p.OnCommand(cmd, entity); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svc.sent) != 1 {
		t.Fatalf("expected 1 command sent, got %d", len(svc.sent))
	}
	var payload map[string]any
	_ = json.Unmarshal(svc.sent[0].Payload, &payload)
	if payload["type"] != "set_brightness" {
		t.Errorf("downstream type: got %v, want set_brightness", payload["type"])
	}
	if payload["brightness"].(float64) != float64(brightness) {
		t.Errorf("downstream brightness: got %v, want %d", payload["brightness"], brightness)
	}
}

// set_segment with no segment field returns an error.
func TestHandleStripCommand_MissingSegment(t *testing.T) {
	p, _ := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "p", DeviceID: "d", EntityID: "e"},
	})
	cmd := makeCommand(map[string]any{"type": "set_segment"})

	err := p.OnCommand(cmd, entity)
	if err == nil {
		t.Fatal("expected error for missing segment, got nil")
	}
}

// set_segment with an index that has no member returns an error.
func TestHandleStripCommand_IndexNotFound(t *testing.T) {
	p, _ := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "p", DeviceID: "d", EntityID: "e"},
	})
	cmd := makeCommand(map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 99, "rgb": []int{1, 2, 3}},
	})

	err := p.OnCommand(cmd, entity)
	if err == nil {
		t.Fatal("expected error for missing index, got nil")
	}
}

// set_segment with a segment that has neither rgb nor brightness returns an error.
func TestHandleStripCommand_NoRGBOrBrightness(t *testing.T) {
	p, _ := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "p", DeviceID: "d", EntityID: "e"},
	})
	cmd := makeCommand(map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 0},
	})

	err := p.OnCommand(cmd, entity)
	if err == nil {
		t.Fatal("expected error for segment with no rgb or brightness, got nil")
	}
}

// A non-set_segment command on a strip entity (e.g. turn_on bypassing CommandFilter
// in a hypothetical future) is silently ignored — the defensive guard.
func TestHandleStripCommand_NonSegmentCommand_Ignored(t *testing.T) {
	p, svc := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "p", DeviceID: "d", EntityID: "e"},
	})
	cmd := makeCommand(map[string]any{"type": "turn_on"})

	if err := p.OnCommand(cmd, entity); err != nil {
		t.Fatalf("expected nil for non-segment command, got: %v", err)
	}
	if len(svc.sent) != 0 {
		t.Errorf("expected no downstream commands for turn_on on strip, got %d", len(svc.sent))
	}
}

// Invalid JSON payload returns an unmarshal error.
func TestHandleStripCommand_InvalidJSON(t *testing.T) {
	p, _ := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "p", DeviceID: "d", EntityID: "e"},
	})
	cmd := types.Command{ID: "bad", Payload: []byte(`not json`)}

	err := p.OnCommand(cmd, entity)
	if err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
}

// Entity with no strip_members meta returns an error.
func TestHandleStripCommand_NoMeta_ReturnsError(t *testing.T) {
	p, _ := newTestPluginWithCommands(t)

	entity := types.Entity{ID: "strip-no-meta", Domain: "light_strip"}
	cmd := makeCommand(map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 0, "rgb": []int{1, 2, 3}},
	})

	err := p.OnCommand(cmd, entity)
	if err == nil {
		t.Fatal("expected error for missing strip_members meta, got nil")
	}
}

// Downstream SendCommand error is propagated.
func TestHandleStripCommand_SendCommandError_Propagated(t *testing.T) {
	p, svc := newTestPluginWithCommands(t)
	svc.err = errors.New("network down")

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "p", DeviceID: "d", EntityID: "e"},
	})
	cmd := makeCommand(map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 0, "rgb": []int{255, 0, 0}},
	})

	err := p.OnCommand(cmd, entity)
	if err == nil || err.Error() != "network down" {
		t.Fatalf("expected propagated error, got: %v", err)
	}
}

// Downstream command ID is derived from the original command ID.
func TestHandleStripCommand_CommandIDDerivation(t *testing.T) {
	p, svc := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "p", DeviceID: "d", EntityID: "e"},
	})
	cmd := types.Command{
		ID:      "original-cmd",
		Payload: mustMarshal(map[string]any{"type": "set_segment", "segment": map[string]any{"index": 0, "rgb": []int{1, 2, 3}}}),
	}

	if err := p.OnCommand(cmd, entity); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.sent[0].ID != "original-cmd-seg" {
		t.Errorf("downstream ID: got %q, want %q", svc.sent[0].ID, "original-cmd-seg")
	}
}

// Downstream command EntityType is always "light".
func TestHandleStripCommand_EntityTypeIsLight(t *testing.T) {
	p, svc := newTestPluginWithCommands(t)

	entity := makeStripEntity([]stripMember{
		{Index: 0, PluginID: "p", DeviceID: "d", EntityID: "e"},
	})
	cmd := makeCommand(map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 0, "rgb": []int{0, 0, 255}},
	})

	if err := p.OnCommand(cmd, entity); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.sent[0].EntityType != "light" {
		t.Errorf("EntityType: got %q, want %q", svc.sent[0].EntityType, "light")
	}
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
