// Package buildinfo holds version information injected at build time.
package buildinfo

// Set via -ldflags "-X github.com/chris-reynier/stkr/internal/buildinfo.Version=..." etc.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
