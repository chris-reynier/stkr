package cli

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/app"
	"github.com/chris-reynier/stkr/internal/buildinfo"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/update"
)

type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	// UpdateAvailable comes from the cached update check, never the network.
	UpdateAvailable string `json:"update_available,omitempty"`
}

func newVersionCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print version information",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{annotationNoHome: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			v := versionInfo{Version: buildinfo.Version, Commit: buildinfo.Commit, Date: buildinfo.Date, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
			// Cached result of the last update check only; no network call.
			if home, err := opts.resolveHome(); err == nil && !update.IsDev(v.Version) {
				if info, ok := service.CachedUpdateInfo(cmd.Context(), filepath.Join(home, app.DBName), v.Version); ok && info.Available {
					v.UpdateAvailable = info.Latest
				}
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), v)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stkr %s\ncommit: %s\nbuilt: %s\ngo: %s %s/%s\n", v.Version, v.Commit, v.Date, v.Go, v.OS, v.Arch)
			if v.UpdateAvailable != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "update available: %s (run `stkr update`)\n", v.UpdateAvailable)
			}
			return nil
		},
	}
}
