//go:build linux

package tunnel

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestTranslateNetlinkError_Nil(t *testing.T) {
	if err := TranslateNetlinkError(nil, "create", "gre0"); err != nil {
		t.Errorf("nil input should return nil, got %v", err)
	}
}

func TestTranslateNetlinkError_Errno(t *testing.T) {
	cases := []struct {
		name   string
		errno  syscall.Errno
		check  func(error) bool
		substr string
	}{
		{"exists", syscall.EEXIST, IsTunnelExists, "already exists"},
		{"not found", syscall.ENODEV, IsTunnelNotFound, "not found"},
		{"perm", syscall.EPERM, IsPermission, "CAP_NET_ADMIN"},
		{"access", syscall.EACCES, IsPermission, "CAP_NET_ADMIN"},
		{"invalid", syscall.EINVAL, nil, "invalid configuration"},
		{"notsupp", syscall.EOPNOTSUPP, nil, "operation not supported"},
		{"busy", syscall.EBUSY, nil, "device busy"},
		{"netdown", syscall.ENETDOWN, nil, "network is down"},
		{"addrinuse", syscall.EADDRINUSE, nil, "address already in use"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TranslateNetlinkError(c.errno, "create", "gre0")
			if got == nil {
				t.Fatalf("expected error, got nil")
			}
			if c.check != nil && !c.check(got) {
				t.Errorf("predicate failed for %v: %v", c.errno, got)
			}
			if !strings.Contains(got.Error(), c.substr) {
				t.Errorf("want error to contain %q, got %q", c.substr, got.Error())
			}
		})
	}
}

func TestTranslateNetlinkError_Unknown(t *testing.T) {
	// An errno we don't handle specifically falls through to the generic branch.
	got := TranslateNetlinkError(syscall.EIO, "create", "gre0")
	var te *TunnelError
	if !errors.As(got, &te) {
		t.Fatalf("expected *TunnelError wrapping EIO, got %T", got)
	}
	if te.Op != "create" || te.Tunnel != "gre0" {
		t.Errorf("wrong op/name: %+v", te)
	}
	if !errors.Is(got, syscall.EIO) {
		t.Errorf("expected Is(EIO) to be true")
	}
}

func TestTranslateNetlinkError_NonErrno(t *testing.T) {
	plain := errors.New("kaboom")
	got := TranslateNetlinkError(plain, "delete", "gre1")
	var te *TunnelError
	if !errors.As(got, &te) {
		t.Fatalf("expected *TunnelError, got %T", got)
	}
	if !errors.Is(got, plain) {
		t.Errorf("expected Unwrap to reach the original error")
	}
}

func TestWrapValidationError(t *testing.T) {
	if got := WrapValidationError("name", "x", nil); got != nil {
		t.Errorf("nil input should yield nil, got %v", got)
	}

	existing := &ValidationError{Field: "name", Value: "y", Message: "bad"}
	if got := WrapValidationError("other", "z", existing); got != existing {
		t.Errorf("existing ValidationError should pass through unchanged, got %v", got)
	}

	plain := errors.New("parse failed")
	wrapped := WrapValidationError("local", "not-an-ip", plain)
	var ve *ValidationError
	if !errors.As(wrapped, &ve) {
		t.Fatalf("expected *ValidationError, got %T", wrapped)
	}
	if ve.Field != "local" || ve.Value != "not-an-ip" || ve.Message != "parse failed" {
		t.Errorf("wrong fields: %+v", ve)
	}
}

func TestIsTransientError(t *testing.T) {
	for _, e := range []syscall.Errno{syscall.EBUSY, syscall.ENETDOWN, syscall.EAGAIN, syscall.ETIMEDOUT} {
		if !IsTransientError(e) {
			t.Errorf("errno %v should be transient", e)
		}
	}
	for _, e := range []syscall.Errno{syscall.EPERM, syscall.EINVAL, syscall.ENODEV} {
		if IsTransientError(e) {
			t.Errorf("errno %v should NOT be transient", e)
		}
	}
	if IsTransientError(errors.New("not an errno")) {
		t.Errorf("plain error should not be transient")
	}
	if IsTransientError(nil) {
		t.Errorf("nil should not be transient")
	}
}

func TestIsFatalError(t *testing.T) {
	// custom types
	if !IsFatalError(&PermissionError{}) {
		t.Errorf("permission errors are fatal")
	}
	if !IsFatalError(&ValidationError{}) {
		t.Errorf("validation errors are fatal")
	}

	// errnos
	for _, e := range []syscall.Errno{syscall.EPERM, syscall.EACCES, syscall.EINVAL, syscall.EOPNOTSUPP} {
		if !IsFatalError(e) {
			t.Errorf("errno %v should be fatal", e)
		}
	}
	for _, e := range []syscall.Errno{syscall.EBUSY, syscall.EAGAIN} {
		if IsFatalError(e) {
			t.Errorf("errno %v should NOT be fatal", e)
		}
	}

	if IsFatalError(nil) {
		t.Errorf("nil should not be fatal")
	}
	if IsFatalError(errors.New("plain")) {
		t.Errorf("plain error should not be fatal")
	}
}

func TestErrorHint(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		substr string
	}{
		{"nil", nil, ""},
		{"permission", &PermissionError{}, "sudo"},
		{"exists", &TunnelExistsError{Name: "gre0"}, "gretun list"},
		{"notfound", &TunnelNotFoundError{Name: "gre0"}, "gretun list"},
		{"notsupp errno", syscall.EOPNOTSUPP, "modprobe"},
		{"busy errno", syscall.EBUSY, "may be in use"},
		{"invalid errno", syscall.EINVAL, "TTL"},
		{"addrinuse errno", syscall.EADDRINUSE, "already in use"},
		{"no hint", errors.New("mystery"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ErrorHint(c.err)
			if c.substr == "" {
				if got != "" {
					t.Errorf("expected empty hint, got %q", got)
				}
				return
			}
			if !strings.Contains(got, c.substr) {
				t.Errorf("want hint containing %q, got %q", c.substr, got)
			}
		})
	}
}

func TestFormatError(t *testing.T) {
	if got := FormatError(nil); got != "" {
		t.Errorf("nil should yield empty string, got %q", got)
	}

	// Permission error should include a Hint line.
	perm := &PermissionError{Op: "create", Tunnel: "gre0", Message: "CAP_NET_ADMIN required"}
	got := FormatError(perm)
	if !strings.Contains(got, perm.Error()) {
		t.Errorf("format should include underlying error")
	}
	if !strings.Contains(got, "Hint:") {
		t.Errorf("format should include Hint for permission error")
	}

	// Plain error has no hint: just the message.
	plain := errors.New("boom")
	if got := FormatError(plain); got != "boom" {
		t.Errorf("plain error should format as-is, got %q", got)
	}
}

func TestTunnelError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("inner")
	te := &TunnelError{Op: "create", Tunnel: "gre0", Message: "failed", Err: inner}
	s := te.Error()
	for _, want := range []string{"create", "gre0", "failed", "inner"} {
		if !strings.Contains(s, want) {
			t.Errorf("Error() should contain %q, got %q", want, s)
		}
	}
	if te.Unwrap() != inner {
		t.Errorf("Unwrap() should return inner error")
	}

	// No inner: message omits wrapped form.
	te2 := &TunnelError{Op: "delete", Tunnel: "gre1", Message: "gone"}
	if s := te2.Error(); strings.Contains(s, "%!v") {
		t.Errorf("Error() should handle nil inner cleanly: %q", s)
	}
	if te2.Unwrap() != nil {
		t.Errorf("Unwrap() of error with nil Err should be nil")
	}
}

func TestValidationError_Error(t *testing.T) {
	withValue := &ValidationError{Field: "local", Value: "bad", Message: "not an IP"}
	if !strings.Contains(withValue.Error(), `"bad"`) {
		t.Errorf("Error() should quote the value")
	}

	noValue := &ValidationError{Field: "local", Message: "required"}
	if strings.Contains(noValue.Error(), `""`) {
		t.Errorf("Error() should omit empty quoted value")
	}
	if !strings.Contains(noValue.Error(), "required") {
		t.Errorf("Error() should contain the message")
	}
}

func TestPermissionError_Error(t *testing.T) {
	pe := &PermissionError{Op: "create", Tunnel: "gre0", Message: "need caps"}
	s := pe.Error()
	for _, want := range []string{"permission denied", "create", "gre0", "need caps"} {
		if !strings.Contains(s, want) {
			t.Errorf("Error() should contain %q, got %q", want, s)
		}
	}
}

func TestInvalidTypeError_Error(t *testing.T) {
	// With ExpectedType set.
	ite := &InvalidTypeError{Name: "eth0", ActualType: "device", ExpectedType: "gre"}
	if !strings.Contains(ite.Error(), "gre") || !strings.Contains(ite.Error(), "device") {
		t.Errorf("Error() should mention both actual and expected: %q", ite.Error())
	}

	// Without ExpectedType, falls back to default message.
	ite2 := &InvalidTypeError{Name: "eth0", ActualType: "device"}
	if !strings.Contains(ite2.Error(), "GRE") {
		t.Errorf("Default Error() should mention GRE: %q", ite2.Error())
	}
}

func TestTypePredicates(t *testing.T) {
	if !IsTunnelExists(&TunnelExistsError{}) || IsTunnelExists(errors.New("other")) {
		t.Errorf("IsTunnelExists broken")
	}
	if !IsTunnelNotFound(&TunnelNotFoundError{}) || IsTunnelNotFound(errors.New("other")) {
		t.Errorf("IsTunnelNotFound broken")
	}
	if !IsValidation(&ValidationError{}) || IsValidation(errors.New("other")) {
		t.Errorf("IsValidation broken")
	}
	if !IsPermission(&PermissionError{}) || IsPermission(errors.New("other")) {
		t.Errorf("IsPermission broken")
	}
	if !IsInvalidType(&InvalidTypeError{}) || IsInvalidType(errors.New("other")) {
		t.Errorf("IsInvalidType broken")
	}

	// Nil safety.
	if IsTunnelExists(nil) || IsTunnelNotFound(nil) || IsValidation(nil) || IsPermission(nil) || IsInvalidType(nil) {
		t.Errorf("nil should not match any predicate")
	}
}

func TestEncapTypeName(t *testing.T) {
	cases := map[EncapType]string{
		EncapNone:         "none",
		EncapFOU:          "fou",
		EncapGUE:          "gue",
		EncapType(99):     "none", // unknown falls through to default
	}
	for in, want := range cases {
		if got := encapTypeName(in); got != want {
			t.Errorf("encapTypeName(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestMtuOrDefault(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want int
	}{
		{"explicit MTU wins", Config{MTU: 1400, Encap: EncapFOU}, 1400},
		{"fou default", Config{Encap: EncapFOU}, DefaultFOUMTU},
		{"gue default", Config{Encap: EncapGUE}, DefaultFOUMTU},
		{"none: kernel default", Config{Encap: EncapNone}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mtuOrDefault(c.cfg); got != c.want {
				t.Errorf("mtuOrDefault(%+v) = %d, want %d", c.cfg, got, c.want)
			}
		})
	}
}

func TestIpToString(t *testing.T) {
	if got := ipToString(nil); got != "" {
		t.Errorf("nil IP should be empty, got %q", got)
	}
	if got := ipToString(net.ParseIP("10.0.0.1")); got != "10.0.0.1" {
		t.Errorf("got %q", got)
	}
}

func TestValidateEncap_WarnPorts(t *testing.T) {
	// Port 443 collides with HTTPS/QUIC → warn, not error.
	warn, err := ValidateEncap(Config{Encap: EncapFOU, EncapDport: 443})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(warn, "HTTPS") {
		t.Errorf("expected warning about HTTPS collision, got %q", warn)
	}

	// Non-colliding port → no warn.
	warn, err = ValidateEncap(Config{Encap: EncapFOU, EncapDport: 5555})
	if err != nil || warn != "" {
		t.Errorf("unexpected warn/err for benign port: %q / %v", warn, err)
	}
}

func TestValidateEncap_MissingDport(t *testing.T) {
	if _, err := ValidateEncap(Config{Encap: EncapFOU}); err == nil {
		t.Error("expected error when EncapDport is 0 with FOU")
	}
	if _, err := ValidateEncap(Config{Encap: EncapGUE}); err == nil {
		t.Error("expected error when EncapDport is 0 with GUE")
	}
}

func TestValidateEncap_UnknownType(t *testing.T) {
	if _, err := ValidateEncap(Config{Encap: EncapType(42), EncapDport: 5555}); err == nil {
		t.Error("expected error for unknown encap type")
	}
}

func TestValidateEncap_MTUBounds(t *testing.T) {
	for _, mtu := range []int{100, 575, 9001, 20000} {
		if _, err := ValidateEncap(Config{Encap: EncapNone, MTU: mtu}); err == nil {
			t.Errorf("mtu %d should fail", mtu)
		}
	}
	for _, mtu := range []int{0, 576, 1468, 9000} {
		if _, err := ValidateEncap(Config{Encap: EncapNone, MTU: mtu}); err != nil {
			t.Errorf("mtu %d should pass, got %v", mtu, err)
		}
	}
}

