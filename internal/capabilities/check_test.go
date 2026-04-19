//go:build linux

package capabilities

import (
	"os"
	"strings"
	"testing"
)

func TestCheckNetAdmin_Linux(t *testing.T) {
	err := CheckNetAdmin()
	if os.Geteuid() == 0 {
		if err != nil {
			t.Errorf("running as root should return nil, got %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("non-root should return an error")
	}
	msg := err.Error()
	for _, want := range []string{"sudo", "CAP_NET_ADMIN"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %q", want, msg)
		}
	}
}
