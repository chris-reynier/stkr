// Command stkr is the STKR task stack manager.
package main

import (
	"os"

	"github.com/chris-reynier/stkr/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
