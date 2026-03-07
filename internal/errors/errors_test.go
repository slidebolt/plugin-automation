package errors

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(ErrOffline, "device is offline")

	if err.Type != ErrOffline {
		t.Errorf("expected error type %q, got %q", ErrOffline, err.Type)
	}

	if err.Message != "device is offline" {
		t.Errorf("expected message 'device is offline', got %q", err.Message)
	}

	if err.Cause != nil {
		t.Error("expected no cause for new error")
	}
}

func TestWrap(t *testing.T) {
	underlying := errors.New("connection refused")
	err := Wrap(ErrTimeout, "connection timed out", underlying)

	if err.Type != ErrTimeout {
		t.Errorf("expected error type %q, got %q", ErrTimeout, err.Type)
	}

	if err.Message != "connection timed out" {
		t.Errorf("expected message 'connection timed out', got %q", err.Message)
	}

	if err.Cause != underlying {
		t.Error("expected cause to be the underlying error")
	}
}

func TestPluginError_Error(t *testing.T) {
	err := New(ErrOffline, "device not responding")
	expected := "[device_offline] device not responding"

	if err.Error() != expected {
		t.Errorf("expected error string %q, got %q", expected, err.Error())
	}
}

func TestPluginError_ErrorWithCause(t *testing.T) {
	cause := errors.New("network unreachable")
	err := Wrap(ErrOffline, "device not responding", cause)

	errStr := err.Error()
	if errStr == "" {
		t.Error("expected non-empty error string")
	}

	if !contains(errStr, "device_offline") {
		t.Error("expected error string to contain error type")
	}

	if !contains(errStr, "device not responding") {
		t.Error("expected error string to contain message")
	}

	if !contains(errStr, "network unreachable") {
		t.Error("expected error string to contain cause")
	}
}

func TestPluginError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := Wrap(ErrInternal, "wrapped", cause)

	if err.Unwrap() != cause {
		t.Error("expected Unwrap to return the cause")
	}
}

func TestPluginError_ToJSON(t *testing.T) {
	err := New(ErrUnauthorized, "invalid credentials")
	jsonData := err.ToJSON()

	if len(jsonData) == 0 {
		t.Error("expected non-empty JSON data")
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Errorf("failed to unmarshal JSON: %v", err)
	}

	if decoded["type"] != string(ErrUnauthorized) {
		t.Errorf("expected type %q, got %v", ErrUnauthorized, decoded["type"])
	}

	if decoded["message"] != "invalid credentials" {
		t.Errorf("expected message 'invalid credentials', got %v", decoded["message"])
	}
}

func TestPluginError_ToStateField(t *testing.T) {
	err := New(ErrTimeout, "request timed out")
	stateField := err.ToStateField()

	if stateField["error"] == nil {
		t.Fatal("expected error key in state field")
	}

	errorData, ok := stateField["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error data to be a map")
	}

	if errorData["type"] != string(ErrTimeout) {
		t.Errorf("expected error type %q, got %v", ErrTimeout, errorData["type"])
	}

	if errorData["message"] != "request timed out" {
		t.Errorf("expected message 'request timed out', got %v", errorData["message"])
	}
}

func TestIsType(t *testing.T) {
	err := New(ErrOffline, "device offline")

	if !IsType(err, ErrOffline) {
		t.Error("expected IsType to return true for ErrOffline")
	}

	if IsType(err, ErrTimeout) {
		t.Error("expected IsType to return false for ErrTimeout")
	}

	// Test with non-PluginError
	regularErr := errors.New("regular error")
	if IsType(regularErr, ErrOffline) {
		t.Error("expected IsType to return false for non-PluginError")
	}
}

func TestNewOfflineError(t *testing.T) {
	cause := errors.New("connection refused")
	err := NewOfflineError(cause)

	if err.Type != ErrOffline {
		t.Errorf("expected type ErrOffline, got %q", err.Type)
	}

	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

func TestNewUnauthorizedError(t *testing.T) {
	cause := errors.New("401 Unauthorized")
	err := NewUnauthorizedError(cause)

	if err.Type != ErrUnauthorized {
		t.Errorf("expected type ErrUnauthorized, got %q", err.Type)
	}

	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

func TestNewTimeoutError(t *testing.T) {
	cause := errors.New("deadline exceeded")
	err := NewTimeoutError(cause)

	if err.Type != ErrTimeout {
		t.Errorf("expected type ErrTimeout, got %q", err.Type)
	}

	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

func TestNewInvalidPayloadError(t *testing.T) {
	cause := errors.New("invalid json")
	err := NewInvalidPayloadError(cause)

	if err.Type != ErrInvalidPayload {
		t.Errorf("expected type ErrInvalidPayload, got %q", err.Type)
	}

	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

func TestNewInternalError(t *testing.T) {
	cause := errors.New("panic recovered")
	err := NewInternalError(cause)

	if err.Type != ErrInternal {
		t.Errorf("expected type ErrInternal, got %q", err.Type)
	}

	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
