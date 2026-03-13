//go:build linux

package tunnel

import (
	"errors"
	"fmt"
	"syscall"
)

// TranslateNetlinkError converts common netlink/syscall errors into
// user-friendly custom error types with actionable messages.
func TranslateNetlinkError(err error, op string, name string) error {
	if err == nil {
		return nil
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.EEXIST:
			return &TunnelExistsError{Name: name}
		case syscall.ENODEV:
			return &TunnelNotFoundError{Name: name}
		case syscall.EPERM, syscall.EACCES:
			return &PermissionError{
				Op:      op,
				Tunnel:  name,
				Message: "requires CAP_NET_ADMIN capability or root privileges",
			}

		case syscall.EINVAL:
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "invalid configuration (check IP addresses, key, TTL)",
				Err:     err,
			}

		case syscall.EOPNOTSUPP:
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "operation not supported (is the GRE kernel module loaded? try: modprobe ip_gre)",
				Err:     err,
			}

		case syscall.EBUSY:
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "device busy (tunnel may be in use)",
				Err:     err,
			}

		case syscall.ENETDOWN:
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "network is down",
				Err:     err,
			}

		case syscall.EADDRINUSE:
			return &TunnelError{
				Op:      op,
				Tunnel:  name,
				Message: "address already in use",
				Err:     err,
			}
		}
	}

	return &TunnelError{
		Op:      op,
		Tunnel:  name,
		Message: "operation failed",
		Err:     err,
	}
}

// WrapValidationError wraps an error with field context. If err is already
// a ValidationError, it is returned as-is.
func WrapValidationError(field string, value string, err error) error {
	if err == nil {
		return nil
	}

	if _, ok := err.(*ValidationError); ok {
		return err
	}

	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: err.Error(),
	}
}

// IsTransientError reports whether err is likely transient and could succeed on retry.
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

// IsFatalError reports whether err is fatal and should not be retried.
func IsFatalError(err error) bool {
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

// ErrorHint returns actionable advice for resolving an error, or empty string
// if no specific hint is available.
func ErrorHint(err error) string {
	if err == nil {
		return ""
	}

	if IsPermission(err) {
		return "Run with sudo or grant CAP_NET_ADMIN: sudo setcap cap_net_admin+ep $(which gretun)"
	}

	if IsTunnelExists(err) {
		return "Use 'gretun list' to see existing tunnels, or 'gretun delete --name <name>' to remove it first"
	}

	if IsTunnelNotFound(err) {
		return "Use 'gretun list' to see available tunnels"
	}

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
