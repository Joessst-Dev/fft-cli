package main

import (
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const apiDescribeLong = `Describe an API operation: what it does, what it needs, and what to send it.

Everything comes from the spec, so this works offline and needs no project.

  fft api describe getPickJob
  fft api describe addPickJob -o json | jq -r .sampleBody

The EXAMPLE BODY is synthesized from the request schema — the spec ships 1,556
field-level examples and not one request-body example, so there was nothing to
copy. It is a body you can send: every required field is there, and the values
come from the spec's own examples wherever it has one.

To get just that body, use --example on the command itself:

  fft api addPickJob --example > job.json`

func newAPIDescribeCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <operationId>",
		Short: "Show an operation's parameters, permissions and sample body",
		Long:  apiDescribeLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		ValidArgsFunction: completeOperationID,

		RunE: func(cmd *cobra.Command, args []string) error {
			op, err := findOperation(args[0])
			if err != nil {
				return err
			}

			if deps.Printer.Format() != output.Table {
				// -o json is the machine's answer, so it is the whole record: every
				// parameter, the explode flag included, and the sample body as a string.
				return deps.Printer.Render(output.Rows{}, view(cmd.Root(), op))
			}

			writeOperation(cmd.OutOrStdout(), cmd.Root(), op, deps.Printer.Style())
			return nil
		},
	}
}
