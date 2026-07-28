package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const routingStrategyCreateLong = `Create a routing strategy.

The body needs only a name (nameLocalized); the API fills in a default root node and
global configuration, which you then shape with 'update'. A created strategy is a
draft — it is not live until you 'activate' it.

  fft routing strategy create --example > s.json
  $EDITOR s.json
  fft routing strategy create --file s.json

--file - reads the body from stdin.

A create is never retried: if the API answers 500 the strategy may still have been
created, and sending it again would leave you with two.`

func newRoutingStrategyCreateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
	)

	cmd := &cobra.Command{
		Use:   "create --file <file>",
		Short: "Create a routing strategy",
		Long:  routingStrategyCreateLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "postRoutingStrategy"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example needs no project, credentials or network — answer it first. The
			// body is the spec-synthesized one, so it never drifts from the schema.
			if example {
				op, ok := api.LookupOperation("postRoutingStrategy")
				if !ok {
					return fmt.Errorf("no metadata for postRoutingStrategy")
				}
				return printExample(cmd, op)
			}

			if file == "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--file is required: run 'fft routing strategy create --example' for a body to start from")}
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

			answer, err := sendEntity(ctx, c, "create the routing strategy", doc,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().PostRoutingStrategyWithBody(ctx, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			deps.Printer.Notef("Created routing strategy %s.", createdID(answer))
			return renderStrategy(deps, answer)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the strategy ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}

// createdID is the new entity's id, or a stand-in when the API answered without one.
// Shared by the routing create commands, whose "Created X <id>." notice needs the one
// thing the user did not already know.
func createdID(raw []byte) string {
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err == nil && created.ID != "" {
		return created.ID
	}
	return "(the API returned no id)"
}
