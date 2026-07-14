package main

import (
	"context"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const connectionUpdateLong = `Replace a connection with the contents of a JSON file.

This is a PUT, and there is no PATCH: the connection becomes what the file says
and loses anything the file omits. Unlike a facility, you cannot reach for the
safer verb — there isn't one — so the way to change one field is to read the
whole connection, edit it, and send it back:

  fft connection get 3f9c... --facility BER-01 -o json > c.json
  $EDITOR c.json
  fft connection update 3f9c... --facility BER-01 --file c.json

fft supplies the version. The API locks optimistically and carries the version
in the body rather than in an If-Match header, so fft reads the connection first
to learn the current one and retries once if somebody wrote in between.

The connection the API returns has no top-level "type" — it lives inside
"target" — while the body the API accepts requires both. fft fills the missing
one in from the target rather than making you do it, so the round trip above
works as written.

--if-version skips the read: fft sends the version you name and the API rejects
it if it is stale. That is one request instead of two, and a clean failure
instead of a silent overwrite, which is what a CI job wants.`

func newConnectionUpdateCmd(deps *Deps) *cobra.Command {
	var (
		facility string
		file     string
		version  versionFlag
	)

	cmd := &cobra.Command{
		Use:     "update <id> --facility <id> --file <file>",
		Short:   "Replace a connection (PUT)",
		Long:    connectionUpdateLong,
		Args:    usageArgs(cobra.ExactArgs(1)),
		Aliases: []string{"replace"},

		Annotations: map[string]string{annotationOperationID: "updateFacilityConnection"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(cmd, "facility"); err != nil {
				return err
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}
			if err := version.check(); err != nil {
				return err
			}

			body, err := connectionBody(deps, file)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			var (
				source = client.FacilityRef(facility)
				id     = args[0]
				raw    []byte
			)

			get := func(ctx context.Context) (entityDoc, int, error) {
				return getConnectionDoc(ctx, c, source, id)
			}

			put := func(ctx context.Context, doc entityDoc, v int) (entityDoc, error) {
				doc["version"] = v

				answer, err := sendEntity(ctx, c, "update connection "+id, doc,
					func(ctx context.Context, body io.Reader) (*http.Response, error) {
						return c.API().UpdateFacilityConnectionWithBody(ctx, source, id,
							&api.UpdateFacilityConnectionParams{}, contentTypeJSON, body)
					})
				if err != nil {
					return nil, err
				}
				// Captured on the way past: UpdateVersioned is generic over the decoded
				// entity, and -o json must print the bytes the API sent rather than fft's
				// re-encoding of them.
				raw = answer
				return nil, nil
			}

			// A PUT: whatever was read, the file wins. Which is also why --if-version
			// demands the *whole* connection in the file — there is no read for a partial
			// one to be merged into.
			replace := func(doc *entityDoc) error {
				*doc = body
				return nil
			}

			if _, err := client.UpdateVersioned(ctx, get, put, replace, version.value()); err != nil {
				return err
			}

			deps.Printer.Notef("Updated connection %s on facility %s.", id, source)
			return renderConnection(deps, raw)
		},
	}

	f := cmd.Flags()
	registerFacilityFlag(cmd, &facility)
	f.StringVar(&file, "file", "", "JSON file holding the whole connection ('-' for stdin)")
	version.register(f)

	return cmd
}
