package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const connectionCreateLong = `Create a connection out of a facility.

The body must carry a "type" — SUPPLIER, MANAGED_FACILITY or CUSTOMER — and a
"target" that agrees with it. The type is the discriminator the API matches the
rest of the body against, and a SUPPLIER or MANAGED_FACILITY target must name
the facility the edge goes to. fft checks all of that before it sends anything,
because the API's answer to a body it dislikes is a 400 that names no field.

--example prints a body you can edit and send straight back, and --type chooses
which of the three it prints:

  fft connection create --facility BER-01 --example --type MANAGED_FACILITY > c.json
  $EDITOR c.json
  fft connection create --facility BER-01 --file c.json

--file - reads the body from stdin.

A create is never retried. If the API answers 500 the connection may still have
been created, and sending the request again would risk a second edge between the
same two facilities.`

// The example bodies, one per type.
//
// They are hand-written, and this is one of the few places where that beats the
// synthesized body from the spec — the reasoning is the same as stockCreateExample's,
// but the evidence here is blunter. tools/specgen cannot make sense of a
// discriminated oneOf, and what it produces for this operation is:
//
//	{"carrierKey": "DHL_V2", "target": {"type": "SUPPLIER"}, "type": "CUSTOMER"}
//
// which contradicts itself — the discriminator says CUSTOMER, the target says
// SUPPLIER — and names no facility for the supplier to be. It is not a body that can
// be sent. An example that does not work is worse than no example, because the user
// finds out one 400 later and does not know which half to doubt.
const (
	connectionSupplierExample = `{
  "type": "SUPPLIER",
  "target": {
    "type": "SUPPLIER",
    "facilityRef": "8f14e45f-ceea-467a-9575-25a1b5c8b3a1"
  },
  "carrierKey": "DHL_V2",
  "carrierName": "DHL",
  "fallbackTransitTime": {
    "minTransitDays": 1,
    "maxTransitDays": 3
  }
}
`

	connectionManagedFacilityExample = `{
  "type": "MANAGED_FACILITY",
  "target": {
    "type": "MANAGED_FACILITY",
    "facilityRef": "8f14e45f-ceea-467a-9575-25a1b5c8b3a1"
  },
  "carrierKey": "DHL_V2",
  "carrierName": "DHL",
  "fallbackTransitTime": {
    "minTransitDays": 1,
    "maxTransitDays": 2
  }
}
`

	// The customer target names no facility, and is not missing one: the consumer is
	// where the goods stop, not a place that fulfils.
	connectionCustomerExample = `{
  "type": "CUSTOMER",
  "target": {
    "type": "CUSTOMER"
  },
  "carrierKey": "DHL_V2",
  "carrierName": "DHL",
  "fallbackTransitTime": {
    "minTransitDays": 1,
    "maxTransitDays": 3
  }
}
`
)

// connectionExample is the sample body for one type.
func connectionExample(typ string) (string, error) {
	switch typ {
	case typeSupplier:
		return connectionSupplierExample, nil
	case typeManagedFacility:
		return connectionManagedFacilityExample, nil
	case typeCustomer:
		return connectionCustomerExample, nil
	default:
		return "", exitcode.UsageError{Err: fmt.Errorf("unknown --type %q: want one of %s",
			typ, strings.Join(connectionTypes(), ", "))}
	}
}

func newConnectionCreateCmd(deps *Deps) *cobra.Command {
	var (
		facility string
		file     string
		example  bool
		typ      string
	)

	cmd := &cobra.Command{
		Use:   "create --facility <id> --file <file>",
		Short: "Create a connection",
		Long:  connectionCreateLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "createConnectionToFacility"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example is answered before anything else: it needs no project, no
			// credentials and no network — not even --facility, since the body says
			// nothing about the facility it will leave.
			if example {
				body, err := connectionExample(strings.ToUpper(typ))
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(cmd.OutOrStdout(), body)
				return err
			}

			if err := requireFlag(cmd, "facility"); err != nil {
				return err
			}
			if file == "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--file is required: run 'fft connection create --example' for a body to start from")}
			}

			doc, err := connectionBody(deps, file)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			source := client.FacilityRef(facility)

			raw, err := sendEntity(ctx, c, "create the connection", doc,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().CreateConnectionToFacilityWithBody(ctx, source, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			// The notice names the id the API minted, which is the one thing the user did
			// not already know and the thing every following command needs.
			deps.Printer.Notef("Created connection %s on facility %s.", createdConnection(raw), source)
			return renderConnection(deps, raw)
		},
	}

	f := cmd.Flags()
	registerFacilityFlag(cmd, &facility)
	f.StringVar(&file, "file", "", "JSON file holding the connection ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	f.StringVar(&typ, "type", typeSupplier,
		"With --example, the kind of connection to print: "+strings.Join(connectionTypes(), ", "))

	cmd.MarkFlagsMutuallyExclusive("file", "example")

	// --type chooses which example to print, and does nothing else: the type of a
	// connection fft *sends* comes from the body, because that is where the API reads
	// it. So `--file c.json --type CUSTOMER` is refused rather than quietly ignored —
	// a flag that silently does nothing is discovered weeks later by someone whose
	// connections have all been suppliers. Same argument as checkMaxItems.
	cmd.MarkFlagsMutuallyExclusive("file", "type")

	registerEnumCompletion(cmd, "type", connectionTypes())

	return cmd
}

// createdConnection is the new connection's id, or a stand-in when the API answered
// without one. A create that says "Created connection ." helps nobody.
func createdConnection(raw []byte) string {
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err == nil && created.ID != "" {
		return created.ID
	}
	return "(the API returned no id)"
}
