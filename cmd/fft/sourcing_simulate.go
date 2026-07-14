package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const sourcingSimulateLong = `Ask the router how it would fulfil an order.

This changes nothing. It creates no order, reserves no stock, and books no
carrier — it runs the routing engine against a hypothetical order and hands back
the options. It is a POST because the order does not fit in a query string, not
because it writes, and fft knows the difference: it works on a read-only project.

The body is an order: who it goes to, and what is in it.

  fft sourcing simulate --example > order.json
  $EDITOR order.json
  fft sourcing simulate --file order.json --results 3

--file - reads the body from stdin.

The API returns *one* option unless it is asked for more, which is rarely what
somebody asking to see their options wants — so --results defaults to 3. The
answer is kept: 'fft sourcing get <id>' reads the same run back later, and the
id is on stderr.

An empty answer means the order cannot be routed at all. It is not an error and
it is not "no matches" — it is the router saying there is no way to fulfil this.`

// sourcingExample is the order a simulation starts from.
//
// Hand-written, for the reason spelled out at connectionSupplierExample and at
// stockCreateExample: what tools/specgen synthesizes from this schema is a body
// assembled from every field's example value, and for a tree this deep — consumer,
// addresses, line items, articles — that is not a body anyone can send. The schema
// marks only `consumer` required, but an order with no line items is a question with
// no content, so the example carries one.
const sourcingExample = `{
  "order": {
    "consumer": {
      "addresses": [
        {
          "type": "POSTAL_ADDRESS",
          "street": "Hauptstraße",
          "houseNumber": "1",
          "postalCode": "10115",
          "city": "Berlin",
          "country": "DE"
        }
      ]
    },
    "orderLineItems": [
      {
        "article": {
          "tenantArticleId": "ARTICLE-123",
          "title": "A thing worth shipping"
        },
        "quantity": 1
      }
    ]
  }
}
`

// The bounds the spec puts on the optimisation knobs. The API's answer to a value
// outside them is a 400 with no field named, so fft refuses them first.
const (
	// maxResults is optimizationHints.requestedResultCount's ceiling.
	maxResults = 20

	// defaultResults is what fft asks for when the user does not say.
	//
	// The API's own default is 1. A user who typed "show me my options" and got exactly
	// one has been answered with a number, not with options — so fft asks for a few. It
	// is a default and not a floor: --results 1 gets one.
	defaultResults = 3
)

func newSourcingSimulateCmd(deps *Deps) *cobra.Command {
	var (
		file       string
		example    bool
		results    int
		investment float64
		attributes bool
	)

	cmd := &cobra.Command{
		Use:     "simulate --file <file>",
		Short:   "Ask the router how an order would be fulfilled",
		Long:    sourcingSimulateLong,
		Args:    usageArgs(cobra.NoArgs),
		Aliases: []string{"options"},

		Annotations: map[string]string{annotationOperationID: "createSourcingOptionsRequest"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example needs no project, no credentials and no network, so it is answered
			// before anything that does.
			if example {
				_, err := fmt.Fprint(cmd.OutOrStdout(), sourcingExample)
				return err
			}

			if file == "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--file is required: run 'fft sourcing simulate --example' for an order to start from")}
			}

			doc, err := sourcingRequest(deps, file, cmd, results, investment, attributes)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := sendEntity(ctx, c, "simulate the sourcing", doc,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().CreateSourcingOptionsRequestWithBody(ctx, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			// The run's id, on stderr, because it is how the answer is read back and it is
			// not part of the data anybody is piping into jq.
			var res sourcingResponse
			if err := json.Unmarshal(raw, &res); err == nil && res.ID != "" {
				deps.Printer.Notef("Sourcing run %s. Read it again with 'fft sourcing get %s'.", res.ID, res.ID)
			}

			return renderSourcing(deps, raw)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the order ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample order and exit")
	f.IntVar(&results, "results", defaultResults,
		fmt.Sprintf("How many alternative options to ask for, 1–%d", maxResults))
	f.Float64Var(&investment, "investment", 0,
		"How hard the router should look, above 0 and up to 1. Higher is better and slower")
	f.BoolVar(&attributes, "listing-attributes", false,
		"Include each listing's custom attributes in the answer (for debugging)")

	cmd.MarkFlagsMutuallyExclusive("file", "example")
	for _, flag := range []string{"results", "investment", "listing-attributes"} {
		cmd.MarkFlagsMutuallyExclusive("example", flag)
	}

	return cmd
}

// sourcingRequest reads the order and folds the optimisation flags into it.
//
// The flags are written into the body rather than sent alongside it, because that is
// where the API takes them. A file that already carries optimizationHints keeps them
// unless the user overrode them on the command line: the flag is the more specific
// statement of intent, and silently ignoring one the user typed is its own kind of
// lie. --results is the exception in one direction — it has a default, so it is only
// imposed on a file that said nothing.
func sourcingRequest(deps *Deps, path string, cmd *cobra.Command, results int, investment float64, attributes bool) (entityDoc, error) {
	doc, err := sourcingBody(deps, path)
	if err != nil {
		return nil, err
	}

	if results < 1 || results > maxResults {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			"--results must be between 1 and %d, and %d is not", maxResults, results)}
	}

	hints := subDoc(doc, "optimizationHints")

	// The default is only applied to a file that did not speak for itself. A file that
	// asked for 10 options and a user who did not type --results should get 10.
	if _, given := hints["requestedResultCount"]; !given || cmd.Flags().Changed("results") {
		hints["requestedResultCount"] = results
	}
	doc["optimizationHints"] = hints

	if cmd.Flags().Changed("investment") {
		// Above zero, not at it: the spec's minimum is exclusive, so --investment 0 is a
		// 400 rather than "do not bother optimising".
		if investment <= 0 || investment > 1 {
			return nil, exitcode.UsageError{Err: fmt.Errorf(
				"--investment must be above 0 and at most 1, and %g is not", investment)}
		}
		doc["resourceInvestment"] = map[string]any{"level": investment}
	}

	if cmd.Flags().Changed("listing-attributes") {
		info := subDoc(doc, "additionalInfo")
		info["includeListingCustomAttributes"] = attributes
		doc["additionalInfo"] = info
	}

	return doc, nil
}

// sourcingBody reads the order and checks the one thing the API requires and its 400
// does not name.
func sourcingBody(deps *Deps, path string) (entityDoc, error) {
	raw, err := readBody(deps, path)
	if err != nil {
		return nil, err
	}

	doc, err := decodeDoc(raw, path)
	if err != nil {
		return nil, exitcode.UsageError{Err: err}
	}

	order, ok := doc["order"].(map[string]any)
	if !ok {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			"%s has no %q object: there is nothing to route", path, "order")}
	}
	if _, ok := order["consumer"]; !ok {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			"%s has no %q inside %q: the router needs to know where the order is going",
			path, "consumer", "order")}
	}
	return doc, nil
}

// subDoc is the nested object at key, or a fresh one — so that a flag can be folded
// into a body that never mentioned the object it belongs in.
func subDoc(doc entityDoc, key string) map[string]any {
	if sub, ok := doc[key].(map[string]any); ok {
		return sub
	}
	return map[string]any{}
}
