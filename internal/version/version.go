package version

// Version is the application version string, set via -ldflags at build time.
var Version = "dev"

// Commit is the git commit hash, set via -ldflags at build time.
var Commit = "none"

// BuildTime is the UTC timestamp of the build, set via -ldflags at build time.
var BuildTime = "unknown"
