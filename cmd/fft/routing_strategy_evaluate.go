package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const routingStrategyEvaluateLong = `Dry-run a routing strategy against an order.

This walks the strategy for the order in the file and reports the path the engine
would take — each node it entered and whether that node's config passed. It reserves
nothing and creates nothing; it is a what-if, safe to run against the live strategy.

  fft routing strategy evaluate 3f9c... --file order.json
  fft routing strategy evaluate 3f9c... --example > order.json

The body is an order (the same shape 'fft order create' takes). The table lists the
evaluated path; -o json carries the full result, including the evaluated config.

--file - reads the order from stdin.`

func newRoutingStrategyEvaluateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
	)

	cmd := &cobra.Command{
		Use:   "evaluate <id> --file <order.json>",
		Short: "Dry-run a strategy against an order",
		Long:  routingStrategyEvaluateLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "evaluateRoutingStrategy"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if example {
				op, ok := api.LookupOperation("evaluateRoutingStrategy")
				if !ok {
					return fmt.Errorf("no metadata for evaluateRoutingStrategy")
				}
				return printExample(cmd, op)
			}

			if file == "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--file is required: it holds the order to evaluate (run with --example for one to start from)")}
			}

			body, err := readBody(deps, file)
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
			answer, err := sendDoc(ctx, c, "evaluate routing strategy "+id, body,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().EvaluateRoutingStrategyWithBody(ctx, id, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}
			return renderEvaluation(deps, answer)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the order to evaluate ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample order body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}

// evaluationResult is the table's model of a RoutingStrategyEvaluationResult: the path
// the engine walked. The evaluated config is in the JSON but not the table — it is a
// whole node config, not a row.
type evaluationResult struct {
	EvaluatedPath []struct {
		Type             string            `json:"type"`
		NameLocalized    map[string]string `json:"nameLocalized"`
		Ref              string            `json:"ref"`
		EvaluationResult string            `json:"evaluationResult"`
	} `json:"evaluatedPath"`
}

// renderEvaluation renders the dry-run: the API's own JSON under -o json, a table of
// the evaluated path otherwise.
func renderEvaluation(deps *Deps, raw []byte) error {
	// A 2xx with no body is a legitimate answer fft must not fail on — the write half
	// of this resource tolerates one via sendDoc, and a read that already succeeded
	// should not be turned into a decode error. In practice the endpoint always returns
	// a result; this keeps the renderer symmetric with renderStrategy either way.
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var res evaluationResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("decode the evaluation result: %w", err)
	}

	// An empty path is a real answer for a single-object response, so the API's
	// document is still printed under -o json rather than replaced with `[]`. Only the
	// table has nothing to show.
	if len(res.EvaluatedPath) == 0 {
		deps.Printer.Warnf("The strategy evaluated to an empty path for this order.")
		if deps.Printer.Format() != output.Table {
			return deps.Printer.RenderRaw(output.Rows{}, raw)
		}
		return deps.Printer.Empty("path elements")
	}

	style := deps.Printer.Style()
	rows := make([][]string, 0, len(res.EvaluatedPath))
	for _, e := range res.EvaluatedPath {
		rows = append(rows, []string{
			field(style, e.Type),
			field(style, localeName(e.NameLocalized)),
			field(style, e.Ref),
			field(style, e.EvaluationResult),
		})
	}

	return deps.Printer.RenderRaw(
		output.Rows{Headers: []string{"TYPE", "NAME", "REF", "RESULT"}, Rows: rows}, raw)
}
