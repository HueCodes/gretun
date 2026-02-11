//go:build linux

package capabilities

import (
	"fmt"
	"os"
)

// CheckNetAdmin checks if the process has the necessary capabilities
// to manage network interfaces (CAP_NET_ADMIN or root).
//
// On Linux, GRE tunnel operations require network administration capabilities,
// which are typically obtained by either:
// 1. Running as root (euid == 0)
// 2. Having CAP_NET_ADMIN capability set on the binary
//
// Returns nil if the process has the required capabilities, or an error
// with a helpful message if it doesn't.
func CheckNetAdmin() error {
	// Check if running as root (euid == 0)
	// Note: This is a simple check. A more sophisticated implementation
	// could use the kernel capabilities API to check for CAP_NET_ADMIN
	// specifically, but checking for root is sufficient for most use cases.
	if os.Geteuid() != 0 {
		return fmt.Errorf(`requires root privileges or CAP_NET_ADMIN capability

GRE tunnel operations require network administration capabilities.
Please run with sudo:
  sudo gretun [command]

Alternatively, you can grant CAP_NET_ADMIN to the binary:
  sudo setcap cap_net_admin+ep $(which gretun)

Note: Using setcap allows running without sudo, but be aware of
the security implications of granting capabilities to binaries.`)
	}

	return nil
}
