package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const routingCategoryCreateLong = `Create a routing node config category.

The body needs a name (nameLocalized) and a colour.

  fft routing category create --example > c.json
  $EDITOR c.json
  fft routing category create --file c.json

--file - reads the body from stdin. A create is never retried.`

func newRoutingCategoryCreateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
	)

	cmd := &cobra.Command{
		Use:   "create --file <file>",
		Short: "Create a routing category",
		Long:  routingCategoryCreateLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "postRoutingStrategyNodeConfigCategory"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			if example {
				op, ok := api.LookupOperation("postRoutingStrategyNodeConfigCategory")
				if !ok {
					return fmt.Errorf("no metadata for postRoutingStrategyNodeConfigCategory")
				}
				return printExample(cmd, op)
			}

			if file == "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--file is required: run 'fft routing category create --example' for a body to start from")}
			}

			raw, err := readBody(deps, file)
			if err != nil {
				return err
			}
			doc, err := decodeDoc(raw, file)
			if err != nil {
				return exitcode.UsageError{Err: err}
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			answer, err := sendEntity(ctx, c, "create the routing category", doc,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().PostRoutingStrategyNodeConfigCategoryWithBody(ctx, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			deps.Printer.Notef("Created routing category %s.", createdID(answer))
			return renderCategory(deps, answer)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the category ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}
