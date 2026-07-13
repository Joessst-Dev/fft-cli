package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const stockActionsLong = `Run one action against the tenant's stocks.

This is POST /api/stocks/actions, and it is COLLECTION-level: there is no stock
id in the path. The action itself says which stocks it applies to — by location,
by article, or by a list of ids — and the API finds them. It is not a per-stock
action like a pickjob's, and there is no 'fft stock actions <id>'.

That is what makes it useful: "delete every stock at this location" is one
request, not one request per stock plus a search to find them.

  fft stock actions --example > action.json
  $EDITOR action.json
  fft stock actions --file action.json

The five actions, all discriminated on "name":

  DELETE_BY_LOCATIONS   delete every stock at these locations
  DELETE_BY_PRODUCTS    delete every stock of these articles
  DELETE_BY_IDS         delete these stocks
  MOVE_TO_LOCATION      move stock to another location
  UPDATE_VERSIONLESS    upsert without an optimistic-locking version

Four of the five delete things, and none of them asks first — the file IS the
confirmation. Read it before you send it.`

// The actions POST /api/stocks/actions accepts (swagger:78059). They are the
// oneOf's discriminator, and an action naming none of them is a 400 that does not
// say which of the five shapes it failed to match.
var stockActionNames = []string{
	"DELETE_BY_LOCATIONS",
	"DELETE_BY_PRODUCTS",
	"DELETE_BY_IDS",
	"MOVE_TO_LOCATION",
	"UPDATE_VERSIONLESS",
}

// stockActionsExample is hand-written, not synthesized: the action schema is an
// abstract oneOf with no discriminator mapping, so the synthesizer can only produce
// {} for it. See stockCreateExample.
const stockActionsExample = `{
  "action": {
    "name": "DELETE_BY_LOCATIONS",
    "facilityRef": "8f14e45f-ceea-467a-9575-25a1b5c8b3a1",
    "locationRefs": ["shelf-a-12", "shelf-a-13"]
  }
}
`

func newStockActionsCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
	)

	cmd := &cobra.Command{
		Use:     "actions --file <file>",
		Short:   "Run one action against the tenant's stocks (collection-level)",
		Long:    stockActionsLong,
		Args:    usageArgs(cobra.NoArgs),
		Aliases: []string{"action"},

		Annotations: map[string]string{annotationOperationID: "performStocksActions"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			if example {
				_, err := fmt.Fprint(cmd.OutOrStdout(), stockActionsExample)
				return err
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}

			raw, err := readBody(deps, file)
			if err != nil {
				return err
			}

			name, err := checkStockAction(raw, file)
			if err != nil {
				return err
			}

			ok, err := confirmDestructive(deps, fmt.Sprintf(
				"Run %s against the tenant's stocks? This cannot be undone.", name))
			if err != nil {
				return err
			}
			if !ok {
				deps.Printer.Notef("Aborted; %s was not run.", name)
				return nil
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			answer, err := sendDoc(ctx, c, "run the stock action "+name, raw,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().PerformStocksActionsWithBody(ctx, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			deps.Printer.Notef("Ran %s.", name)
			return renderStockAction(deps, answer)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", `JSON file holding {"action": {...}} ('-' for stdin)`)
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}

// stockActionResult is the API's answer (swagger:78087): the action that ran, and
// a result whose shape depends on which one it was.
type stockActionResult struct {
	Name string `json:"name"`
}

// renderStockAction prints what the action did.
//
// The result is a union of three shapes — one per family of action — and none of
// them is a stock, so there is no stock table to render it into and inventing
// columns for it would be worse than printing the two facts fft is sure of. Under
// -o json the API's own answer is printed in full, which is where a script should
// be reading it from anyway.
func renderStockAction(deps *Deps, raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var result stockActionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		// The action has already run. Failing on the *answer* would tell the user to
		// re-run a deletion that succeeded.
		deps.Printer.Warnf("The action ran, but its answer could not be read.")
		return nil
	}

	rows := output.Rows{
		Headers: []string{"ACTION", "RESULT"},
		Rows:    [][]string{{field(deps.Printer.Style(), result.Name), "see -o json for the details"}},
	}
	return deps.Printer.RenderRaw(rows, raw)
}

// checkStockAction refuses a body the API would reject, and reports the action's
// name so that the confirmation can say what is about to be run.
//
// The deprecated plural "actions" array is not accepted: the API replaced it with
// a single "action" (swagger:78074), and a user reaching for the old shape should
// be told so rather than have fft send something the API has stopped promising to
// honour.
func checkStockAction(raw []byte, path string) (string, error) {
	var body struct {
		Action  *struct{ Name string } `json:"action"`
		Actions []json.RawMessage      `json:"actions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", exitcode.UsageError{Err: fmt.Errorf(
			`%s is not a stock action: it must be an object like {"action": {...}}: %w`, path, err)}
	}

	if body.Action == nil {
		if len(body.Actions) > 0 {
			return "", exitcode.UsageError{Err: fmt.Errorf(
				`%s uses the deprecated "actions" array: the API takes a single "action" object now — {"action": {"name": "...", ...}}`,
				path)}
		}
		return "", exitcode.UsageError{Err: fmt.Errorf(
			`%s has no "action": run 'fft stock actions --example' for a body to start from`, path)}
	}

	name := strings.ToUpper(strings.TrimSpace(body.Action.Name))
	switch {
	case name == "":
		return "", exitcode.UsageError{Err: fmt.Errorf(
			`%s: the action has no "name", and it is the discriminator the API matches the rest of the body against — want one of %s`,
			path, strings.Join(stockActionNames, ", "))}

	case !slices.Contains(stockActionNames, name):
		return "", exitcode.UsageError{Err: fmt.Errorf(
			"%s: unknown action %q: want one of %s", path, body.Action.Name, strings.Join(stockActionNames, ", "))}
	}

	return name, nil
}
