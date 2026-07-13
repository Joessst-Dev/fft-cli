package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const stockCreateLong = `Create a stock: a quantity of one article at one facility.

The short form covers the common case:

  fft stock create --tenant-article-id 4711 --facility BER-01 --value 12

--facility takes your own tenantFacilityId or the platform's UUID, and fft sends
whichever of the two the API expects for the value you gave it.

For everything else — a location, an expiry date, scannable codes, stock
properties — use a file:

  fft stock create --example > stock.json
  $EDITOR stock.json
  fft stock create --file stock.json

The API requires only tenantArticleId and value. It targets a facility with one
of three fields — "facility", "facilityRef" or "tenantFacilityId" — and marks
NONE of them as required, so a body with none of them (or with two) is accepted
by the schema and rejected by the server with an error that does not say which.
fft therefore checks that exactly one is set, before sending anything.

A create is never retried. If the API answers 500 the stock may still have been
created, and sending the request again would risk creating a second one; fft
tells you instead of guessing.`

// stockCreateExample is a body that is valid as it stands: the two required
// fields, the facility selector, and the handful that make a stock useful.
//
// It is deliberately NOT the body tools/specgen synthesizes, and this is the one
// place in fft where a hand-written example beats the generated one. The
// synthesizer emits every *required* field, and the schema marks none of the three
// facility selectors required — so the generated body is
// {"tenantArticleId":"string","value":1}, which fft's own one-selector check
// refuses and the API rejects with an error that does not say why. The knowledge
// that a stock needs a facility is knowledge the schema does not contain, so it has
// to live here.
//
// Same reason for stockUpsertExample and stockActionsExample. Everywhere else,
// --example prints the synthesized body (see printCommandExample).
const stockCreateExample = `{
  "tenantArticleId": "4711",
  "value": 12,
  "facility": { "tenantFacilityId": "BER-01" },
  "locationRef": "shelf-a-12",
  "scannableCodes": ["4001234567890"],
  "properties": {
    "expiry": "2026-12-31"
  }
}
`

// The three ways a stock names its facility (swagger:78374). The schema marks
// none of them required, which is why they are listed here and checked by hand.
var stockFacilityFields = []string{"facility", client.KeyFacilityRef, client.KeyTenantFacilityID}

func newStockCreateCmd(deps *Deps) *cobra.Command {
	var (
		file     string
		example  bool
		article  string
		facility string
		value    int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a stock",
		Long:  stockCreateLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "createStock"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example is answered before anything else: it needs no project, no
			// credentials and no network, and a user reaching for it is usually a user
			// who has not set those up yet.
			if example {
				_, err := fmt.Fprint(cmd.OutOrStdout(), stockCreateExample)
				return err
			}

			doc, err := stockCreateBody(cmd, deps, file, article, facility, value)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := sendEntity(ctx, c, "create the stock", doc,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().CreateStockWithBody(ctx, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			// The notice names the stock the API *created*: the answer carries the
			// platform id, which is the one thing the user did not already know and the
			// thing every subsequent command needs.
			deps.Printer.Notef("Created stock %s.", createdStock(raw, doc))
			return renderStock(deps, raw)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the stock ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	f.StringVar(&article, "tenant-article-id", "", "The article this is stock of")
	f.StringVar(&facility, "facility", "", "The facility, by tenantFacilityId or platform UUID")
	f.IntVar(&value, "value", 0, "How many there are")

	cmd.MarkFlagsMutuallyExclusive("file", "example")
	for _, flag := range []string{"tenant-article-id", "facility", "value"} {
		cmd.MarkFlagsMutuallyExclusive("file", flag)
		cmd.MarkFlagsMutuallyExclusive("example", flag)
	}

	return cmd
}

// stockCreateBody builds the request body from either the file or the flags, and
// refuses one the API would reject for reasons it will not explain.
func stockCreateBody(cmd *cobra.Command, deps *Deps, file, article, facility string, value int) (entityDoc, error) {
	// Changed(), not the value: `--file ""` is a path the user gave and got wrong,
	// and it deserves "read : no such file" rather than a message about the three
	// flags they did not pass.
	if cmd.Flags().Changed("file") {
		raw, err := readBody(deps, file)
		if err != nil {
			return nil, err
		}

		doc, err := decodeDoc(raw, file)
		if err != nil {
			return nil, exitcode.UsageError{Err: err}
		}
		if err := checkStockFacility(doc, file); err != nil {
			return nil, err
		}
		return doc, nil
	}

	return stockFromFlags(cmd, article, facility, value)
}

// stockFromFlags builds the body of the short form.
//
// Every flag is required here, and each is checked through Changed() rather than
// against its value: --value 0 is a legitimate quantity (the shelf is empty, and
// saying so is the whole point of the write), and guarding on the zero value would
// make `--value 0` mean "you forgot --value".
func stockFromFlags(cmd *cobra.Command, article, facility string, value int) (entityDoc, error) {
	missing := make([]string, 0, 3)
	for _, flag := range []string{"tenant-article-id", "facility", "value"} {
		if !cmd.Flags().Changed(flag) {
			missing = append(missing, "--"+flag)
		}
	}

	if len(missing) > 0 {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			"%s %s required: pass all three, or describe the stock in a file with --file",
			strings.Join(missing, ", "), plural(len(missing), "is", "are"))}
	}

	if value < 0 {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			"--value cannot be negative, and %d is: a stock of -1 is not a thing the API can represent", value)}
	}

	key, id := client.FacilitySelector(facility)
	if key == "" {
		return nil, exitcode.UsageError{Err: fmt.Errorf("--facility cannot be empty")}
	}

	return entityDoc{
		"tenantArticleId": article,
		"value":           value,
		// The object form, not the two deprecated scalars: facilityRef and
		// tenantFacilityId are both marked deprecated on StockForCreation in favour of
		// this (swagger:78398). fft picks the right key inside it from the shape of
		// what the user typed.
		"facility": map[string]string{key: id},
	}, nil
}

// checkStockFacility enforces what the schema does not: a stock names exactly one
// facility.
//
// StockForCreation marks none of "facility", "facilityRef" and "tenantFacilityId"
// as required, so a body with zero of them passes schema validation and is
// rejected by the server with a message that does not name the field — and a body
// with two is ambiguous in a way the server resolves silently. Both are worth
// catching here, where the message can name the file and the fields.
func checkStockFacility(doc entityDoc, path string) error {
	var found []string
	for _, name := range stockFacilityFields {
		if v, ok := doc[name]; ok && v != nil {
			found = append(found, name)
		}
	}

	switch len(found) {
	case 1:
		return nil

	case 0:
		return exitcode.UsageError{Err: fmt.Errorf(
			"%s does not say which facility the stock is at: set exactly one of %s (%q is the current spelling; the other two are deprecated)",
			path, strings.Join(quoted(stockFacilityFields), ", "), "facility")}

	default:
		slices.Sort(found)
		return exitcode.UsageError{Err: fmt.Errorf(
			"%s names the facility %d times, with %s: set exactly one of them",
			path, len(found), strings.Join(quoted(found), " and "))}
	}
}

// createdStock describes the new stock for the notice: its platform id if the API
// told us one, else the article the request asked for. Falling back to the request
// keeps the sentence honest when the answer had no body.
func createdStock(raw []byte, sent entityDoc) string {
	if created, err := decodeDoc(raw, "the API's answer"); err == nil {
		if id := docString(created, "id"); id != "" {
			return id
		}
	}

	if article := docString(sent, "tenantArticleId"); article != "" {
		return "of article " + article
	}
	return "(unidentified)"
}

// quoted renders a list of field names as a reader expects to see them.
func quoted(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, fmt.Sprintf("%q", n))
	}
	return out
}

// plural picks the verb. "--value is required" and "--facility, --value are
// required" — a message that gets this wrong reads as though fft is guessing.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
