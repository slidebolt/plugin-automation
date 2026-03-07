// Package errors provides structured error types for the automation plugin.
// These error types enable proper error mapping to user-friendly messages in the UI.
package errors

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrorType represents the category of error that occurred.
type ErrorType string

const (
	// ErrOffline indicates the device is not reachable (network or power issue).
	ErrOffline ErrorType = "device_offline"

	// ErrUnauthorized indicates authentication or authorization failure.
	ErrUnauthorized ErrorType = "unauthorized"

	// ErrTimeout indicates a communication timeout occurred.
	ErrTimeout ErrorType = "timeout"

	// ErrInvalidPayload indicates the event/command payload was malformed.
	ErrInvalidPayload ErrorType = "invalid_payload"

	// ErrInternal indicates an unexpected internal error.
	ErrInternal ErrorType = "internal_error"
)

// PluginError represents a structured error with a type and message.
type PluginError struct {
	Type    ErrorType `json:"type"`
	Message string    `json:"message"`
	Cause   error     `json:"-"`
}

// Error implements the error interface.
func (e *PluginError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Type, e.Message)
}

// Unwrap returns the underlying cause for error chaining.
func (e *PluginError) Unwrap() error {
	return e.Cause
}

// ToJSON converts the error to a JSON representation for storage in entity state.
func (e *PluginError) ToJSON() json.RawMessage {
	data, _ := json.Marshal(e)
	return data
}

// ToStateField converts the error to a map for inclusion in entity Data.Reported.
func (e *PluginError) ToStateField() map[string]interface{} {
	return map[string]interface{}{
		"error": map[string]interface{}{
			"type":    string(e.Type),
			"message": e.Message,
		},
	}
}

// New creates a new PluginError with the given type and message.
func New(errType ErrorType, message string) *PluginError {
	return &PluginError{
		Type:    errType,
		Message: message,
	}
}

// Wrap wraps an existing error with a PluginError.
func Wrap(errType ErrorType, message string, cause error) *PluginError {
	return &PluginError{
		Type:    errType,
		Message: message,
		Cause:   cause,
	}
}

// IsType checks if an error is a PluginError of the given type.
func IsType(err error, errType ErrorType) bool {
	var pluginErr *PluginError
	if errors.As(err, &pluginErr) {
		return pluginErr.Type == errType
	}
	return false
}

// Predefined error constructors for common scenarios.

// NewOfflineError creates an error for offline devices.
func NewOfflineError(cause error) *PluginError {
	return Wrap(ErrOffline, "Device is not responding", cause)
}

// NewUnauthorizedError creates an error for authentication failures.
func NewUnauthorizedError(cause error) *PluginError {
	return Wrap(ErrUnauthorized, "Authentication failed", cause)
}

// NewTimeoutError creates an error for timeouts.
func NewTimeoutError(cause error) *PluginError {
	return Wrap(ErrTimeout, "Communication timed out", cause)
}

// NewInvalidPayloadError creates an error for invalid payloads.
func NewInvalidPayloadError(cause error) *PluginError {
	return Wrap(ErrInvalidPayload, "Invalid payload format", cause)
}

// NewInternalError creates an error for internal failures.
func NewInternalError(cause error) *PluginError {
	return Wrap(ErrInternal, "Internal error occurred", cause)
}
