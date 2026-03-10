package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	pluginerrors "github.com/slidebolt/plugin-automation/internal/errors"
	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

// mockEventSink implements runner.EventSink for testing
type mockEventSink struct {
	events []types.InboundEvent
}

func (m *mockEventSink) EmitEvent(evt types.InboundEvent) error {
	m.events = append(m.events, evt)
	return nil
}

func TestPluginAutomationPlugin_OnInitialize(t *testing.T) {
	plugin := &PluginAutomationPlugin{}
	mockSink := &mockEventSink{}

	config := runner.Config{
		EventSink: mockSink,
	}
	state := types.Storage{}

	manifest, newState := plugin.OnInitialize(config, state)

	if manifest.ID != "plugin-automation" {
		t.Errorf("expected ID 'plugin-automation', got %q", manifest.ID)
	}

	if manifest.Name != "Plugin Automation" {
		t.Errorf("expected Name 'Plugin Automation', got %q", manifest.Name)
	}

	if manifest.Version != "1.0.0" {
		t.Errorf("expected Version '1.0.0', got %q", manifest.Version)
	}

	if plugin.sink != mockSink {
		t.Error("expected plugin.sink to be set to mockSink")
	}

	if newState.Meta != state.Meta {
		t.Error("expected state to be unchanged")
	}
}

func TestPluginAutomationPlugin_WaitReady(t *testing.T) {
	plugin := &PluginAutomationPlugin{}
	ctx := context.Background()

	err := plugin.WaitReady(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestPluginAutomationPlugin_OnHealthCheck(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	status, err := plugin.OnHealthCheck()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if status != "perfect" {
		t.Errorf("expected status 'perfect', got %q", status)
	}
}

func TestPluginAutomationPlugin_OnDeviceDiscover(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	current := []types.Device{}
	devices, err := plugin.OnDeviceDiscover(current)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}

	// Test that existing devices are preserved
	existingDev := types.Device{ID: "existing-device", SourceID: "other-source"}
	current = []types.Device{existingDev}
	devices, err = plugin.OnDeviceDiscover(current)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}

	found := false
	for _, d := range devices {
		if d.ID == "existing-device" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected existing device to be preserved")
	}
}

func TestPluginAutomationPlugin_OnEntityDiscover(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	// Use the plugin ID as device ID, which is what EnsureCoreDevice/EnsureCoreEntities expects
	deviceID := "plugin-automation"
	current := []types.Entity{}
	entities, err := plugin.OnEntityDiscover(deviceID, current)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// The runner.EnsureCoreEntities should add core entities for the management device
	if len(entities) == 0 {
		t.Fatal("expected at least one entity (from core entities)")
	}
}

func TestPluginAutomationPlugin_OnCommand(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	entity := types.Entity{
		ID:        "test-entity",
		LocalName: "Test Switch",
		Data: types.EntityData{
			Reported:  json.RawMessage(`{"state":"OFF"}`),
			Effective: json.RawMessage(`{"state":"OFF"}`),
		},
	}

	cmd := types.Command{
		ID:      "cmd-1",
		Payload: json.RawMessage(`{"action":"turn_on"}`),
	}

	updatedEntity, err := plugin.OnCommand(cmd, entity)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if updatedEntity.ID != entity.ID {
		t.Error("expected entity ID to remain unchanged")
	}
}

func TestPluginAutomationPlugin_OnEvent_Success(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	entity := types.Entity{
		ID:        "test-entity",
		LocalName: "Test Switch",
		Data: types.EntityData{
			Reported:   json.RawMessage(`{"state":"OFF"}`),
			Effective:  json.RawMessage(`{"state":"OFF"}`),
			SyncStatus: "pending",
		},
	}

	payloadJSON, _ := json.Marshal(map[string]interface{}{
		"state": "ON",
	})

	evt := types.Event{
		ID:      "evt-1",
		Payload: payloadJSON,
	}

	updatedEntity, err := plugin.OnEvent(evt, entity)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if updatedEntity.Data.SyncStatus != "synced" {
		t.Errorf("expected SyncStatus 'synced', got %q", updatedEntity.Data.SyncStatus)
	}

	// Verify the payload was marshaled correctly
	var reported map[string]interface{}
	if err := json.Unmarshal(updatedEntity.Data.Reported, &reported); err != nil {
		t.Errorf("failed to unmarshal reported: %v", err)
	}

	if reported["state"] != "ON" {
		t.Errorf("expected state 'ON' in reported, got %v", reported["state"])
	}

	if updatedEntity.Data.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}

	if updatedEntity.Data.UpdatedAt.After(time.Now().UTC()) {
		t.Error("expected UpdatedAt to be in the past or present")
	}
}

func TestPluginAutomationPlugin_OnConfigUpdate(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	state := types.Storage{
		Meta: "test-meta",
		Data: json.RawMessage(`{"key":"value"}`),
	}

	updatedState, err := plugin.OnConfigUpdate(state)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if updatedState.Meta != state.Meta {
		t.Error("expected state meta to remain unchanged")
	}
}

func TestPluginAutomationPlugin_OnDeviceCreate(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	dev := types.Device{
		ID:       "new-device",
		SourceID: "source-123",
	}

	createdDev, err := plugin.OnDeviceCreate(dev)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if createdDev.ID != dev.ID {
		t.Errorf("expected device ID %q, got %q", dev.ID, createdDev.ID)
	}
}

func TestPluginAutomationPlugin_OnDeviceUpdate(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	dev := types.Device{
		ID:       "existing-device",
		SourceID: "source-456",
	}

	updatedDev, err := plugin.OnDeviceUpdate(dev)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if updatedDev.ID != dev.ID {
		t.Errorf("expected device ID %q, got %q", dev.ID, updatedDev.ID)
	}
}

func TestPluginAutomationPlugin_OnDeviceDelete(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	err := plugin.OnDeviceDelete("device-to-delete")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestPluginAutomationPlugin_OnDeviceSearch(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	query := types.SearchQuery{Pattern: "test"}
	existing := []types.Device{
		{ID: "dev1", LocalName: "Test Device"},
	}

	results, err := plugin.OnDeviceSearch(query, existing)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(results) != len(existing) {
		t.Errorf("expected %d results, got %d", len(existing), len(results))
	}
}

func TestPluginAutomationPlugin_OnEntityCreate(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	entity := types.Entity{
		ID:        "new-entity",
		LocalName: "New Entity",
	}

	createdEntity, err := plugin.OnEntityCreate(entity)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if createdEntity.ID != entity.ID {
		t.Errorf("expected entity ID %q, got %q", entity.ID, createdEntity.ID)
	}
}

func TestPluginAutomationPlugin_OnEntityUpdate(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	entity := types.Entity{
		ID:        "existing-entity",
		LocalName: "Updated Entity",
	}

	updatedEntity, err := plugin.OnEntityUpdate(entity)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if updatedEntity.ID != entity.ID {
		t.Errorf("expected entity ID %q, got %q", entity.ID, updatedEntity.ID)
	}
}

func TestPluginAutomationPlugin_OnEntityDelete(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	err := plugin.OnEntityDelete("device-id", "entity-to-delete")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestPluginAutomationPlugin_OnReady(t *testing.T) {
	plugin := &PluginAutomationPlugin{}
	// Should not panic
	plugin.OnReady()
}

func TestPluginAutomationPlugin_OnShutdown(t *testing.T) {
	plugin := &PluginAutomationPlugin{}
	// Should not panic
	plugin.OnShutdown()
}

func TestPluginAutomationPlugin_OnEvent_WithError(t *testing.T) {
	plugin := &PluginAutomationPlugin{}

	entity := types.Entity{
		ID:        "test-entity",
		LocalName: "Test Switch",
		Data: types.EntityData{
			SyncStatus: "pending",
		},
	}

	// Test with valid JSON - should succeed
	evt := types.Event{
		ID:      "evt-1",
		Payload: json.RawMessage(`{"valid": "json"}`),
	}

	updatedEntity, err := plugin.OnEvent(evt, entity)
	if err != nil {
		t.Errorf("expected no error with valid JSON, got %v", err)
	}

	if updatedEntity.Data.SyncStatus != "synced" {
		t.Errorf("expected SyncStatus 'synced', got %q", updatedEntity.Data.SyncStatus)
	}
}

func TestPluginAutomationPlugin_OnEvent_ErrorHandling(t *testing.T) {
	// Verify the error handling logic and error types are properly defined

	// Test error creation
	err := pluginerrors.New(pluginerrors.ErrInternal, "test error")
	if err == nil {
		t.Error("expected error to be created")
	}

	if !pluginerrors.IsType(err, pluginerrors.ErrInternal) {
		t.Error("expected error to be of type ErrInternal")
	}

	// Test error wrapping
	wrappedErr := pluginerrors.Wrap(pluginerrors.ErrOffline, "device offline", err)
	if wrappedErr.Cause != err {
		t.Error("expected wrapped error to have correct cause")
	}

	// Test error state conversion
	stateField := wrappedErr.ToStateField()
	if stateField["error"] == nil {
		t.Error("expected error field in state")
	}

	errorData, ok := stateField["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error data to be a map")
	}

	if errorData["type"] != string(pluginerrors.ErrOffline) {
		t.Errorf("expected error type %q, got %q", pluginerrors.ErrOffline, errorData["type"])
	}

	// Test JSON conversion
	jsonData := wrappedErr.ToJSON()
	if len(jsonData) == 0 {
		t.Error("expected JSON data to be non-empty")
	}
}
