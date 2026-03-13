//go:build linux

package capabilities

import (
	"fmt"
	"os"
)

// CheckNetAdmin verifies that the process has network administration
// privileges (root or CAP_NET_ADMIN), which are required for GRE
// tunnel operations on Linux.
func CheckNetAdmin() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("requires root privileges or CAP_NET_ADMIN capability\n\n" +
			"GRE tunnel operations require network administration capabilities.\n" +
			"Please run with sudo:\n" +
			"  sudo gretun [command]\n\n" +
			"Alternatively, grant CAP_NET_ADMIN to the binary:\n" +
			"  sudo setcap cap_net_admin+ep $(which gretun)")
	}

	return nil
}
