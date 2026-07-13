package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const facilityCreateLong = `Create a facility from a JSON file.

The body must carry a "type": MANAGED_FACILITY or SUPPLIER. It is the
discriminator the API matches the rest of the body against, and it cannot be
changed afterwards — there is no action that turns a supplier into a managed
facility.

--example prints a body you can edit and send straight back:

  fft facility create --example > facility.json
  $EDITOR facility.json
  fft facility create --file facility.json

--file - reads the body from stdin.

A create is never retried. If the API answers 500 the facility may still have
been created, and sending the request again would risk creating a second one;
fft tells you instead of guessing.`

func newFacilityCreateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a facility",
		Long:  facilityCreateLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "addFacility"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example is answered before anything else: it needs no project, no
			// credentials and no network, and a user reaching for it is usually a user
			// who has not set those up yet.
			//
			// The body is the one tools/specgen synthesizes from the schema, not a
			// hand-written one: it carries the discriminator, the address the schema
			// requires, and the spec's own example values — so it stays right when the
			// schema moves, which a constant in this file would not.
			if example {
				return printCommandExample(cmd)
			}

			if file == "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--file is required: run 'fft facility create --example' for a body to start from")}
			}

			doc, err := facilityBody(deps, file)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := sendEntity(ctx, c, "create the facility", doc,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().AddFacilityWithBody(ctx, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			// The notice names the facility the API *created*, not the one the file
			// asked for: the answer carries the platform id, which is the one thing the
			// user did not already know and the thing the next command needs.
			deps.Printer.Notef("Created facility %s.", createdFacility(raw, doc))
			return renderFacility(deps, raw)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the facility ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}

// createdFacility describes the new facility for the notice: its platform id if
// the API told us one, else the name the request asked for. Falling back to the
// request keeps the sentence honest when the answer had no body.
func createdFacility(raw []byte, sent entityDoc) string {
	if created, err := decodeDoc(raw, "the API's answer"); err == nil {
		if id := docString(created, "id"); id != "" {
			return id
		}
	}

	if name := docString(sent, "name"); name != "" {
		return name
	}
	return "(unnamed)"
}
