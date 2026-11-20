package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/buildinfo"
)

type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func newVersionCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print version information",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{annotationNoHome: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			v := versionInfo{buildinfo.Version, buildinfo.Commit, buildinfo.Date, runtime.Version(), runtime.GOOS, runtime.GOARCH}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), v)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stkr %s\ncommit: %s\nbuilt: %s\ngo: %s %s/%s\n", v.Version, v.Commit, v.Date, v.Go, v.OS, v.Arch)
			return nil
		},
	}
}
