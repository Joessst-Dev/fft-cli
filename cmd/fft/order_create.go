package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const orderCreateLong = `Create an order from a JSON file.

The body is an OrderForCreation: it needs an orderDate, a consumer, and at least
one orderLineItem with an article and a quantity. Most orders carry much more — a
delivery address, delivery preferences, a source, tags — but those are the three
the API insists on.

--example prints a minimal body you can edit and send straight back:

  fft order create --example > order.json
  $EDITOR order.json
  fft order create --file order.json

--file - reads the body from stdin.

A create is never retried. If the API answers 500 the order may still have been
created, and sending the request again would risk creating a second one; fft tells
you instead of guessing.`

// orderCreateExample is a body that is valid as it stands: an orderDate, a
// consumer, and one line item with the article and quantity the schema requires.
//
// It is deliberately NOT the body tools/specgen synthesizes. OrderForCreation
// carries a `source` and a `workflowInformation` that are discriminated oneOfs,
// and specgen cannot synthesize a coherent variant for a oneOf — so its body would
// be a shape the API rejects with an error that does not say why. The knowledge of
// what a minimal, sendable order looks like does not live in the schema, so it
// lives here. (Same reason as stockCreateExample and the connection examples.)
const orderCreateExample = `{
  "orderDate": "2026-07-15T08:45:50.525Z",
  "tenantOrderId": "ORD-2026-0001",
  "consumer": {
    "addresses": [
      {
        "street": "Lichtstr.",
        "houseNumber": "50",
        "city": "Cologne",
        "postalCode": "50825",
        "country": "DE",
        "email": "consumer@example.com"
      }
    ]
  },
  "orderLineItems": [
    {
      "article": {
        "tenantArticleId": "SKU-123",
        "title": "Cologne Water"
      },
      "quantity": 1
    }
  ]
}
`

func newOrderCreateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an order",
		Long:  orderCreateLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "addOrder"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example is answered before anything else: it needs no project, no
			// credentials and no network, and a user reaching for it is usually a user
			// who has not set those up yet.
			if example {
				_, err := fmt.Fprint(cmd.OutOrStdout(), orderCreateExample)
				return err
			}

			if file == "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--file is required: run 'fft order create --example' for a body to start from")}
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

			answer, err := sendEntity(ctx, c, "create the order", doc,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().AddOrderWithBody(ctx, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			// The notice names the order the API *created*: the answer carries the
			// platform id, which is the one thing the user did not already know and the
			// thing the next command needs.
			deps.Printer.Notef("Created order %s.", createdOrder(answer, doc))
			return renderOrder(deps, answer)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the order ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}

// createdOrder describes the new order for the notice: its platform id if the API
// told us one, else the tenantOrderId the request asked for. Falling back to the
// request keeps the sentence honest when the answer had no body.
func createdOrder(raw []byte, sent entityDoc) string {
	if created, err := decodeDoc(raw, "the API's answer"); err == nil {
		if id := docString(created, "id"); id != "" {
			return id
		}
	}

	if tenantOrderID := docString(sent, "tenantOrderId"); tenantOrderID != "" {
		return tenantOrderID
	}
	return "(new)"
}
