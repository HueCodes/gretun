//go:build linux

package tunnel

import "fmt"

// TunnelError is the base error type for tunnel operations.
// It provides context about which operation failed and on which tunnel.
type TunnelError struct {
	Op      string // Operation that failed (e.g., "create", "delete", "get")
	Tunnel  string // Tunnel name involved
	Message string // Human-readable error message
	Err     error  // Underlying error, if any
}

// Error returns a human-readable description of the tunnel error.
func (e *TunnelError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s tunnel %s: %s: %v", e.Op, e.Tunnel, e.Message, e.Err)
	}
	return fmt.Sprintf("%s tunnel %s: %s", e.Op, e.Tunnel, e.Message)
}

// Unwrap returns the underlying error, enabling errors.Is and errors.As unwrapping.
func (e *TunnelError) Unwrap() error {
	return e.Err
}

// TunnelExistsError is returned when attempting to create a tunnel that already exists.
type TunnelExistsError struct {
	Name string
}

// Error returns a message indicating that the named tunnel already exists.
func (e *TunnelExistsError) Error() string {
	return fmt.Sprintf("tunnel %s already exists", e.Name)
}

// IsTunnelExists checks if an error is a TunnelExistsError.
func IsTunnelExists(err error) bool {
	_, ok := err.(*TunnelExistsError)
	return ok
}

// TunnelNotFoundError is returned when a tunnel does not exist.
type TunnelNotFoundError struct {
	Name string
}

// Error returns a message indicating that the named tunnel was not found.
func (e *TunnelNotFoundError) Error() string {
	return fmt.Sprintf("tunnel %s not found", e.Name)
}

// IsTunnelNotFound checks if an error is a TunnelNotFoundError.
func IsTunnelNotFound(err error) bool {
	_, ok := err.(*TunnelNotFoundError)
	return ok
}

// ValidationError is returned when input validation fails.
type ValidationError struct {
	Field   string // Field that failed validation
	Value   string // Value that was invalid
	Message string // Validation error message
}

// Error returns a human-readable description of the validation failure.
func (e *ValidationError) Error() string {
	if e.Value != "" {
		return fmt.Sprintf("validation failed for %s (%q): %s", e.Field, e.Value, e.Message)
	}
	return fmt.Sprintf("validation failed for %s: %s", e.Field, e.Message)
}

// IsValidation checks if an error is a ValidationError.
func IsValidation(err error) bool {
	_, ok := err.(*ValidationError)
	return ok
}

// PermissionError is returned when the operation fails due to insufficient permissions.
type PermissionError struct {
	Op      string
	Tunnel  string
	Message string
}

// Error returns a message describing the permission failure.
func (e *PermissionError) Error() string {
	return fmt.Sprintf("permission denied: %s tunnel %s: %s", e.Op, e.Tunnel, e.Message)
}

// IsPermission checks if an error is a PermissionError.
func IsPermission(err error) bool {
	_, ok := err.(*PermissionError)
	return ok
}

// InvalidTypeError is returned when an interface is not a GRE tunnel.
type InvalidTypeError struct {
	Name         string
	ActualType   string
	ExpectedType string
}

// Error returns a message indicating that the interface is not the expected tunnel type.
func (e *InvalidTypeError) Error() string {
	if e.ExpectedType != "" {
		return fmt.Sprintf("%s is not a %s tunnel (type: %s)", e.Name, e.ExpectedType, e.ActualType)
	}
	return fmt.Sprintf("%s is not a GRE tunnel (type: %s)", e.Name, e.ActualType)
}

// IsInvalidType checks if an error is an InvalidTypeError.
func IsInvalidType(err error) bool {
	_, ok := err.(*InvalidTypeError)
	return ok
}
