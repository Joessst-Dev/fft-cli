package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const routingStrategyActionsLong = `Run one action against a routing strategy.

This is the escape hatch for the four structural actions that have no verb of their
own: 'activate' is the fifth, curated separately because it is the one you reach for
by far the most.

  fft routing strategy actions <id> --example > action.json
  $EDITOR action.json
  fft routing strategy actions <id> --file action.json

The five actions, all discriminated on "name":

  ACTIVATE                       make this strategy the live one (see 'activate')
  COPY                           create a new draft from this strategy
  REPLACE_GLOBAL_CONFIGURATION   swap the strategy's global configuration
  REPLACE_NODE                   replace one node in the tree, by id
  REPLACE_CONDITION              replace one condition in the tree, by id

REPLACE_NODE and REPLACE_CONDITION are the id-addressed way to edit one part of the
tree without the whole-strategy PUT that 'update' does. Most carry the strategy's
"version" for optimistic locking, which the --example body already has.`

// The actions POST /api/routing/strategies/{id}/actions accepts. They are the oneOf's
// discriminator (swagger:74037), and an action naming none of them is a 400 that does
// not say which of the five shapes it failed to match.
var routingStrategyActionNames = []string{
	"ACTIVATE",
	"COPY",
	"REPLACE_GLOBAL_CONFIGURATION",
	"REPLACE_NODE",
	"REPLACE_CONDITION",
}

func newRoutingStrategyActionsCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
	)

	cmd := &cobra.Command{
		Use:     "actions <id> --file <file>",
		Short:   "Run one action against a routing strategy",
		Long:    routingStrategyActionsLong,
		Args:    usageArgs(cobra.MaximumNArgs(1)),
		Aliases: []string{"action"},

		// Shared with 'activate', which sends one specific action. Two curated commands
		// over one operationId is the same shape as fft order's cancel/unlock over
		// orderAction: the read-only gate and the shadowing both key on the id, and both
		// commands making the same promise about the same endpoint is correct.
		Annotations: map[string]string{annotationOperationID: "actionsRoutingStrategy"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if example {
				return printCommandExample(cmd)
			}

			if len(args) != 1 {
				return exitcode.UsageError{Err: fmt.Errorf(
					"which strategy? Name one, or run --example for a body to start from")}
			}
			if file == "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--file is required: run 'fft routing strategy actions <id> --example' for a body to start from")}
			}

			raw, err := readBody(deps, file)
			if err != nil {
				return err
			}

			name, err := checkRoutingStrategyAction(raw, file)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			id := args[0]
			answer, err := sendDoc(ctx, c, fmt.Sprintf("run %s against routing strategy %s", name, id), raw,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().ActionsRoutingStrategyWithBody(ctx, id, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			deps.Printer.Notef("Ran %s against routing strategy %s.", name, id)
			return renderStrategy(deps, answer)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the action ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}

// checkRoutingStrategyAction refuses a body the API would reject for a reason it does
// not name, and reports the action's name so the notice can say what ran.
func checkRoutingStrategyAction(raw []byte, path string) (string, error) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", exitcode.UsageError{Err: fmt.Errorf(
			`%s is not a routing strategy action: it must be an object like {"name": "...", ...}: %w`, path, err)}
	}

	name := strings.ToUpper(strings.TrimSpace(body.Name))
	switch {
	case name == "":
		return "", exitcode.UsageError{Err: fmt.Errorf(
			`%s has no "name", and it is the discriminator the API matches the rest of the body against — want one of %s`,
			path, strings.Join(routingStrategyActionNames, ", "))}

	case !slices.Contains(routingStrategyActionNames, name):
		return "", exitcode.UsageError{Err: fmt.Errorf(
			"%s: unknown action %q: want one of %s", path, body.Name, strings.Join(routingStrategyActionNames, ", "))}
	}

	return name, nil
}
