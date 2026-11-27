package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
)

const networkInferLong = `Write a starter fft-network.yaml from what a check infers about the network.

Every value the check would otherwise guess goes into the file with the reason
it was chosen, as a comment above it: the hubs, how stores are grouped into
peers, where the kinds of order come from, and one expectation per country a
hub already covers.

  fft network infer
  fft network infer --out - > proposal.yaml
  fft network infer --snapshot network.json.gz --force

What you keep in the file is pinned. The check follows it, and reports
config-drift when the network moves away from it, so a changed result is a
reviewed change to the file. What you delete is inferred again on every run.
Commit the file; 'fft network config validate' checks it, and 'fft network
check' reads it from the working directory.

It reads the snapshot only and sends no request. It will not overwrite a file
without --force.`

func newNetworkInferCmd(deps *Deps) *cobra.Command {
	var (
		snapshot string
		out      string
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "infer",
		Short: "Write a starter fft-network.yaml from a network snapshot",
		Long:  networkInferLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := snapshotPath(deps, snapshot)
			if err != nil {
				return err
			}
			s, err := network.Load(path)
			if err != nil {
				return err
			}
			text := network.New(s).InferFile(time.Now())

			if out == "-" {
				_, err := deps.Printer.Out().Write(text)
				return err
			}
			if _, err := os.Stat(out); err == nil && !force {
				return exitcode.UsageError{Err: fmt.Errorf(
					"%s exists; pass --force to replace it, or --out - to print the proposal instead", out)}
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			// Readable by others: it is meant to be reviewed and committed, like the
			// code next to it.
			if err := atomicfile.WriteMode(out, text, 0o644, 0o755); err != nil {
				return fmt.Errorf("write %s: %w", out, err)
			}
			deps.Printer.Notef("Wrote %s. Review it, keep what you mean, and commit it.", out)
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&snapshot, "snapshot", "", "Read this snapshot instead of the project's file in the cache directory")
	f.StringVar(&out, "out", network.DefaultConfigFile, "Write the config here; '-' prints it on stdout")
	f.BoolVar(&force, "force", false, "Replace the file if it exists")

	return cmd
}
