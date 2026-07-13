package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const facilityUpdateLong = `Replace a facility with the contents of a JSON file.

This is a PUT: the facility becomes what the file says and loses anything the
file omits. To change one field and leave the rest alone, use 'fft facility
patch'.

The API has no If-Match header — optimistic locking travels in the body as a
"version" field — so fft reads the facility first to learn its current version,
sends that version back, and retries once if someone wrote in between. Your file
does not need a version; fft supplies it.

  fft facility get BER-01 -o json > facility.json
  $EDITOR facility.json
  fft facility update BER-01 --file facility.json

--if-version skips the read: fft sends the version you name and the API answers
409 if it is stale. That is one request instead of two, and a clean failure
instead of a silent overwrite — which is what a CI job wants. (It is
--if-version and never --version: cobra owns --version on the root command.)`

func newFacilityUpdateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
		version versionFlag
	)

	cmd := &cobra.Command{
		Use:   "update <id> --file <file>",
		Short: "Replace a facility (PUT)",
		Long:  facilityUpdateLong,
		// MaximumNArgs and not ExactArgs, because cobra validates the arguments before
		// RunE runs and --example takes no id. The id is demanded below instead, once
		// --example has had its say.
		Args:    usageArgs(cobra.MaximumNArgs(1)),
		Aliases: []string{"replace"},

		Annotations: map[string]string{annotationOperationID: "replaceFacility"},

		RunE: func(cmd *cobra.Command, args []string) error {
			// --example needs no project, no credentials and no network, so it is
			// answered before anything that does.
			if example {
				return printCommandExample(cmd)
			}
			if len(args) != 1 {
				return exitcode.UsageError{Err: fmt.Errorf(
					"which facility? Name one, or run --example for a body to start from")}
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}
			if err := version.check(); err != nil {
				return err
			}

			body, err := facilityBody(deps, file)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			ref := client.FacilityRef(args[0])

			// The raw answer is captured on the way past, because UpdateVersioned is
			// generic over the decoded entity and -o json must print the bytes the API
			// sent, not fft's re-encoding of them.
			var raw []byte

			get := func(ctx context.Context) (entityDoc, int, error) {
				return getFacilityDoc(ctx, c, ref)
			}

			put := func(ctx context.Context, doc entityDoc, v int) (entityDoc, error) {
				doc["version"] = v

				answer, err := sendEntity(ctx, c, "update facility "+ref, doc,
					func(ctx context.Context, body io.Reader) (*http.Response, error) {
						return c.API().ReplaceFacilityWithBody(ctx, ref, contentTypeJSON, body)
					})
				if err != nil {
					return nil, err
				}
				raw = answer
				return nil, nil
			}

			// The mutation is a replacement: whatever was read, the file wins. That is
			// what makes this a PUT — and it is why, with --if-version, the file has to
			// be the *whole* facility: there is no read for it to be merged into.
			replace := func(doc *entityDoc) error {
				*doc = body
				return nil
			}

			if _, err := client.UpdateVersioned(ctx, get, put, replace, version.value()); err != nil {
				return err
			}

			deps.Printer.Notef("Updated facility %s.", ref)
			return renderFacility(deps, raw)
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "JSON file holding the whole facility ('-' for stdin)")
	cmd.Flags().BoolVar(&example, "example", false, "Print a sample request body and exit")
	version.register(cmd.Flags())

	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}
