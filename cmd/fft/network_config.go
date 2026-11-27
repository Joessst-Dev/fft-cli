package main

import (
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/network"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

func newNetworkConfigCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Check and describe the fft-network.yaml format",
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newNetworkConfigSchemaCmd(deps), newNetworkConfigValidateCmd(deps))
	return cmd
}

const networkConfigSchemaLong = `Print the JSON Schema of fft-network.yaml.

Point an editor's YAML support at it for completion and inline errors, or hand it
to an agent before it proposes a change to the file.

  fft network config schema > fft-network.schema.json

The schema checks the file's shape. 'fft network config validate' also checks
what only fft can: that expectation names are unique, that suppressions have not
expired, and that what the file names exists in the network.`

func newNetworkConfigSchemaCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema of fft-network.yaml",
		Long:  networkConfigSchemaLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(_ *cobra.Command, _ []string) error {
			return deps.Printer.RenderDocument(network.ConfigSchema)
		},
	}
}

const networkConfigValidateLong = `Check fft-network.yaml and list every problem in it.

  fft network config validate
  fft network config validate --config ops/fft-network.yaml -o json

It reports, each with its line:

  - what the format does not allow: an unknown field, a value out of range, an
    expectation named like a rule, a suppression without a reason or a date;
  - suppressions that have expired, which no longer suppress anything;
  - with a snapshot (the project's, or --snapshot), every pinned facility,
    selector and suppression that names nothing in the network.

stdout carries the problems and nothing else. Exit 0 means the file is fine; exit
3 means it has problems. It sends no request.`

// configProblem is one row of `config validate`'s answer.
type configProblem struct {
	Line    int    `json:"line,omitempty" yaml:"line,omitempty"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
	Message string `json:"message" yaml:"message"`
}

func newNetworkConfigValidateCmd(deps *Deps) *cobra.Command {
	var (
		configPath string
		snapshot   string
	)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "List every problem in fft-network.yaml",
		Long:  networkConfigValidateLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(_ *cobra.Command, _ []string) error {
			lf, err := network.ReadConfig(configPath)
			var cfgErr *network.ConfigError
			var problems []network.Problem
			switch {
			case errors.As(err, &cfgErr):
				problems = cfgErr.Problems
			case network.IsNotExist(err):
				problems = []network.Problem{{Message: "there is no config file there; write one with fft network infer"}}
			case err != nil:
				return err
			default:
				problems = lf.Expired(time.Now())
				if n := validationNetwork(deps, snapshot); n != nil {
					problems = append(problems, lf.Unmatched(n)...)
				} else {
					deps.Printer.Notef("No snapshot to check it against, so what it names in the network is not checked.")
				}
			}
			slices.SortStableFunc(problems, func(a, b network.Problem) int { return a.Line - b.Line })

			if len(problems) == 0 {
				deps.Printer.Notef("%s is valid.", configPath)
				return deps.Printer.Empty("problems")
			}
			rows := make([][]string, len(problems))
			data := make([]configProblem, len(problems))
			for i, p := range problems {
				line := "-"
				if p.Line > 0 {
					line = strconv.Itoa(p.Line)
				}
				rows[i] = []string{line, p.Path, p.Message}
				data[i] = configProblem(p)
			}
			if err := deps.Printer.Render(output.Rows{Headers: []string{"LINE", "PATH", "PROBLEM"}, Rows: rows}, data); err != nil {
				return err
			}
			return &network.ConfigError{Path: configPath, Problems: problems}
		},
	}

	f := cmd.Flags()
	f.StringVar(&configPath, "config", network.DefaultConfigFile, "The config file to check")
	f.StringVar(&snapshot, "snapshot", "", "Check what the file names against this snapshot instead of the project's")
	return cmd
}

// validationNetwork is the snapshot to check a config's names against: the one
// --snapshot names, or the project's if it has one. Without either, validate
// still checks the file itself.
func validationNetwork(deps *Deps, flag string) *network.Network {
	path, err := snapshotPath(deps, flag)
	if err != nil {
		return nil
	}
	s, err := network.Load(path)
	if err != nil {
		if flag != "" {
			deps.Printer.Warnf("%v", err)
		}
		return nil
	}
	return network.New(s)
}
