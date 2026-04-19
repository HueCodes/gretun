//go:build !linux

package capabilities

import (
	"strings"
	"testing"
)

func TestCheckNetAdmin_NonLinux(t *testing.T) {
	err := CheckNetAdmin()
	if err == nil {
		t.Fatal("non-linux should always return an error")
	}
	if !strings.Contains(err.Error(), "Linux") {
		t.Errorf("error should mention Linux, got %q", err)
	}
}
