package main

import (
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

func newVersionCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the fft version",
		Long: `Print the version, commit and build date of this fft binary.

Honours --output, so a script can read the version as JSON:

  fft version -o json | jq -r .version`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			info := buildinfo.Current()
			return deps.Printer.Render(versionRows(info), info)
		},
	}
}

// versionRows lays the build metadata out as a two-column table, which reads
// better than a header row of four columns nothing else would line up with.
func versionRows(info buildinfo.Info) output.Rows {
	return output.Rows{
		Headers: []string{"FIELD", "VALUE"},
		Rows: [][]string{
			{"version", info.Version},
			{"commit", info.Commit},
			{"built", info.Date},
			{"go", info.GoVersion},
		},
	}
}
