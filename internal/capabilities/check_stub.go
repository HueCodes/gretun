//go:build !linux

package capabilities

import "errors"

// CheckNetAdmin returns an error on non-Linux platforms since
// GRE tunnels are Linux-specific.
func CheckNetAdmin() error {
	return errors.New("GRE tunnels are only supported on Linux")
}
