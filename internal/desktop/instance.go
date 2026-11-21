package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"strings"
)

// InstanceName is the per-data-folder name of the desktop app instance,
// "stkr-desktop-<16 hex>". The hash is over the cleaned absolute data folder
// (case-folded on Windows, where paths are case-insensitive), so two data
// folders can each have one tray while the same folder never has two.
func InstanceName(home string) string {
	return instanceName(home, runtime.GOOS == "windows")
}

func instanceName(home string, foldCase bool) string {
	p := home
	if abs, err := filepath.Abs(home); err == nil {
		p = abs
	}
	p = strings.ReplaceAll(filepath.Clean(p), `\`, "/")
	p = strings.TrimRight(p, "/")
	if foldCase {
		p = strings.ToLower(p)
	}
	sum := sha256.Sum256([]byte(p))
	return "stkr-desktop-" + hex.EncodeToString(sum[:8])
}

// MutexName is the named mutex that guards the single instance.
func MutexName(home string) string { return MutexPrefix + InstanceName(home) }

// MutexPrefix puts the mutex in the session namespace (one tray per sign-in).
const MutexPrefix = `Local\`
