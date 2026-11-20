package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Subdirectories created inside the data directory.
var subdirs = []string{"audio", "prompts", "backups", "logs"}

// ResolveHome returns the data directory: flag, then STKR_HOME, then ~/.stkr.
func ResolveHome(flag string) (string, error) {
	if flag != "" {
		return filepath.Abs(flag)
	}
	if env := os.Getenv("STKR_HOME"); env != "" {
		return filepath.Abs(env)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(h, ".stkr"), nil
}

// EnsureHome creates the data directory and its subdirectories with
// user-only permissions where the OS supports it.
func EnsureHome(home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	for _, d := range subdirs {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			return fmt.Errorf("create %s dir: %w", d, err)
		}
	}
	return nil
}

// Path returns the config.yaml path inside home.
func Path(home string) string { return filepath.Join(home, "config.yaml") }
