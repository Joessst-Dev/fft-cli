package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const connectionDeleteLong = `Delete a connection of a facility.

This removes an edge from the fulfillment graph. Orders stop being sourced along
it immediately, and if it was the only way to reach a target, that target stops
being reachable at all — so this is rather more destructive than it looks, and
it cannot be undone.

fft reads the connection first so that it can ask about it by name rather than
by UUID. -y/--yes answers for you. On a non-interactive terminal there is nobody
to ask, and fft refuses rather than assuming yes.`

func newConnectionDeleteCmd(deps *Deps) *cobra.Command {
	var facility string

	cmd := &cobra.Command{
		Use:     "delete <id> --facility <id>",
		Short:   "Delete a connection",
		Long:    connectionDeleteLong,
		Args:    usageArgs(cobra.ExactArgs(1)),
		Aliases: []string{"rm"},

		Annotations: map[string]string{annotationOperationID: "deleteFacilityConnection"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(cmd, "facility"); err != nil {
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
			)

			// Read it before asking about it. "Delete connection
			// 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10?" is not a question anybody can answer
			// safely; "Delete the SUPPLIER connection to FRA-02 via DHL_V2?" is. One GET
			// is a cheap price for a question that means something.
			//
			// It is skipped under --yes: the user has already said they do not want to be
			// asked, so the request that exists only to phrase the asking has nothing to
			// buy.
			what := fmt.Sprintf("connection %s", id)
			if !deps.AssumeYes {
				who, err := lookupConnection(ctx, c, source, id)
				if err != nil {
					return err
				}
				what = who.String()
			}

			ok, err := confirmDestructive(deps, fmt.Sprintf(
				"Delete %s of facility %s? Orders will stop being routed through it. This cannot be undone.",
				what, source))
			if err != nil {
				return err
			}
			if !ok {
				deps.Printer.Notef("Aborted; connection %s was not deleted.", id)
				return nil
			}

			// A DELETE is idempotent, so Do may safely retry it on a dropped connection.
			if _, err := c.Do(ctx, "delete connection "+id, func(ctx context.Context) (*http.Response, error) {
				return c.API().DeleteFacilityConnection(ctx, source, id)
			}); err != nil {
				return err
			}

			// Nothing on stdout: a delete has no data to emit, and a cheerful sentence
			// there would land in the pipe of anyone who scripted it.
			deps.Printer.Notef("Deleted connection %s of facility %s.", id, source)
			return nil
		},
	}

	registerFacilityFlag(cmd, &facility)

	return cmd
}
