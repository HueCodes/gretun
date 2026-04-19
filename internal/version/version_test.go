package version

import "testing"

func TestDefaults(t *testing.T) {
	// When the binary is built without -ldflags, the package-level vars fall
	// back to the literals declared in version.go. Tests run unlinkered so we
	// can assert those defaults.
	if Version == "" {
		t.Error("Version should have a non-empty default")
	}
	if Commit == "" {
		t.Error("Commit should have a non-empty default")
	}
	if BuildTime == "" {
		t.Error("BuildTime should have a non-empty default")
	}
}

func TestVariablesAreAssignable(t *testing.T) {
	// These vars are package-level because they're set via -ldflags.
	// Round-trip a value through each to confirm they're not constants.
	origV, origC, origT := Version, Commit, BuildTime
	defer func() {
		Version, Commit, BuildTime = origV, origC, origT
	}()

	Version = "1.2.3"
	Commit = "abcdef0"
	BuildTime = "2026-01-01T00:00:00Z"

	if Version != "1.2.3" || Commit != "abcdef0" || BuildTime != "2026-01-01T00:00:00Z" {
		t.Errorf("values didn't round-trip: %q %q %q", Version, Commit, BuildTime)
	}
}
