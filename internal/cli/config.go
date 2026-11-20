package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/chris-reynier/stkr/internal/config"
)

func newConfigCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or change the configuration",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Print the effective configuration (secrets are masked)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, warnings, err := loadConfig(opts)
				if err != nil {
					return err
				}
				warn(cmd.ErrOrStderr(), warnings)
				data, err := cfg.YAML()
				if err != nil {
					return err
				}
				if !opts.json {
					_, err = cmd.OutOrStdout().Write(data)
					return err
				}
				var generic any
				if err := yaml.Unmarshal(data, &generic); err != nil {
					return err
				}
				return printJSON(cmd.OutOrStdout(), generic)
			},
		},
		&cobra.Command{
			Use:         "path",
			Short:       "Print the path of config.yaml",
			Args:        cobra.NoArgs,
			Annotations: map[string]string{annotationNoHome: "true"},
			RunE: func(cmd *cobra.Command, _ []string) error {
				home, err := opts.resolveHome()
				if err != nil {
					return err
				}
				p := config.Path(home)
				if opts.json {
					return printJSON(cmd.OutOrStdout(), map[string]string{"path": p})
				}
				fmt.Fprintln(cmd.OutOrStdout(), p)
				return nil
			},
		},
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Set a configuration value (dotted keys, for example ollama.url)",
			Long: "Set a configuration value. Nested keys use dots, for example ai.provider. " +
				"List values are comma-separated. Invalid values are rejected and nothing is written.",
			Args: func(cmd *cobra.Command, args []string) error {
				if err := cobra.ExactArgs(2)(cmd, args); err != nil {
					return usageError{err}
				}
				return nil
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, warnings, err := loadConfig(opts)
				if err != nil {
					return err
				}
				warn(cmd.ErrOrStderr(), warnings)
				if err := cfg.Set(args[0], args[1]); err != nil {
					return err
				}
				home, err := opts.resolveHome()
				if err != nil {
					return err
				}
				if err := config.Save(config.Path(home), cfg); err != nil {
					return err
				}
				if opts.json {
					b, _ := json.Marshal(map[string]string{"key": args[0], "value": args[1]})
					fmt.Fprintln(cmd.OutOrStdout(), string(b))
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", args[0], args[1])
				return nil
			},
		},
	)
	return cmd
}

func loadConfig(opts *options) (*config.Config, []string, error) {
	home, err := opts.resolveHome()
	if err != nil {
		return nil, nil, err
	}
	return config.Load(config.Path(home))
}
