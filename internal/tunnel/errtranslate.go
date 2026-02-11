//go:build linux

package tunnel

import (
	"errors"
	"fmt"
	"syscall"
)

// TranslateNetlinkError translates common netlink/syscall errors to user-friendly custom error types.
// This helps provide better error messages to users instead of cryptic syscall errors.
func TranslateNetlinkError(err error, op string, name string) error {
	if err == nil {
		return nil
	}

	// Unwrap to get the underlying error
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.EEXIST:
			// File/link already exists
			return &TunnelExistsError{Name: name}

		case syscall.ENODEV:
			// No such device
			return &TunnelNotFoundError{Name: name}

		case syscall.EPERM, syscall.EACCES:
			// Permission denied
			return &PermissionError{
				Op:      op,
				Tunnel:  name,
				Message: "requires CAP_NET_ADMIN capability or root privileges",
			}

		case syscall.EINVAL:
			// Invalid argument - often means bad configuration
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "invalid configuration (check IP addresses, key, TTL)",
				Err:     err,
			}

		case syscall.EOPNOTSUPP:
			// Operation not supported - GRE module might not be loaded
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "operation not supported (is the GRE kernel module loaded? try: modprobe ip_gre)",
				Err:     err,
			}

		case syscall.EBUSY:
			// Device or resource busy
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "device busy (tunnel may be in use)",
				Err:     err,
			}

		case syscall.ENETDOWN:
			// Network is down
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "network is down",
				Err:     err,
			}

		case syscall.EADDRINUSE:
			// Address already in use
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "address already in use",
				Err:     err,
			}
		}
	}

	// If we can't translate it, wrap it in a generic TunnelError
	return &TunnelError{
		Op:      op,
		Tunnel:  name,
		Message: "operation failed",
		Err:     err,
	}
}

// WrapValidationError wraps a validation error with field context.
// If the error is already a ValidationError, it returns it as-is.
// Otherwise, it creates a new ValidationError.
func WrapValidationError(field string, value string, err error) error {
	if err == nil {
		return nil
	}

	// If it's already a ValidationError, return it
	if _, ok := err.(*ValidationError); ok {
		return err
	}

	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: err.Error(),
	}
}

// IsTransientError checks if an error is likely transient and could succeed on retry.
// Transient errors include resource busy, network down, etc.
func IsTransientError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.EBUSY, syscall.ENETDOWN, syscall.EAGAIN, syscall.ETIMEDOUT:
			return true
		}
	}
	return false
}

// IsFatalError checks if an error is fatal and should not be retried.
// Fatal errors include permission denied, not supported, invalid argument, etc.
func IsFatalError(err error) bool {
	// Check for our custom error types that are fatal
	if IsPermission(err) || IsValidation(err) {
		return true
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.EPERM, syscall.EACCES, syscall.EINVAL, syscall.EOPNOTSUPP:
			return true
		}
	}
	return false
}

// ErrorHint provides a helpful hint for resolving an error.
// Returns an empty string if no specific hint is available.
func ErrorHint(err error) string {
	if err == nil {
		return ""
	}

	// Check custom error types first
	if IsPermission(err) {
		return "Run with sudo or grant CAP_NET_ADMIN: sudo setcap cap_net_admin+ep $(which gretun)"
	}

	if IsTunnelExists(err) {
		return "Use 'gretun list' to see existing tunnels, or 'gretun delete --name <name>' to remove it first"
	}

	if IsTunnelNotFound(err) {
		return "Use 'gretun list' to see available tunnels"
	}

	// Check syscall errors
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.EOPNOTSUPP:
			return "Load the GRE kernel module: sudo modprobe ip_gre"
		case syscall.EBUSY:
			return "The tunnel interface may be in use. Try bringing it down first or wait a moment"
		case syscall.EINVAL:
			return "Check that IP addresses are valid and reachable, and TTL is in valid range (0-255)"
		case syscall.EADDRINUSE:
			return "The tunnel address is already in use by another interface"
		}
	}

	return ""
}

// FormatError formats an error with an optional hint for the user.
// This is useful for CLI output to provide actionable information.
func FormatError(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()
	if hint := ErrorHint(err); hint != "" {
		return fmt.Sprintf("%s\n\nHint: %s", msg, hint)
	}
	return msg
}
