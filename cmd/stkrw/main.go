// Command stkrw is the STKR desktop app for Windows: a notification-area
// icon that runs (or attaches to) the local STKR server. Build it with
// -ldflags "-H windowsgui" so no console window opens. On other systems it
// prints a hint and exits 2.
package main

import (
	"os"

	"github.com/chris-reynier/stkr/internal/app"
	"github.com/chris-reynier/stkr/internal/buildinfo"
	"github.com/chris-reynier/stkr/internal/desktop"
)

func main() {
	app.CleanupOldBinary()
	os.Exit(desktop.Main(os.Args[1:], os.Stderr, buildinfo.Version))
}
