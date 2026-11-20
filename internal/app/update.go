package app

import (
	"os"

	"github.com/chris-reynier/stkr/internal/update"
)

// CleanupOldBinary removes the stkr.exe.old file a previous Windows update
// left behind. It runs at every start and is silent.
func CleanupOldBinary() {
	if exe, err := os.Executable(); err == nil {
		update.CleanupOld(exe)
	}
}
