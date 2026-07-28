package main

import (
	"context"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const routingStrategyActivateLong = `Make a routing strategy live.

Only one strategy is ever 'inUse'. Activating this one takes over from whichever was
live before, and the engine starts routing with it immediately.

  fft routing strategy activate 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10

Activation is versioned: fft reads the strategy to learn its current version, sends
it with the request, and retries once if somebody wrote in between. --if-version
skips that read — fft sends the version you name and the API rejects it with a 409 if
it is stale.`

func newRoutingStrategyActivateCmd(deps *Deps) *cobra.Command {
	var version versionFlag

	cmd := &cobra.Command{
		Use:   "activate <id>",
		Short: "Make a routing strategy live",
		Long:  routingStrategyActivateLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "actionsRoutingStrategy"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := version.check(); err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			id := args[0]
			var answer []byte

			get := func(ctx context.Context) (entityDoc, int, error) {
				return getStrategyDoc(ctx, c, id)
			}

			put := func(ctx context.Context, doc entityDoc, v int) (entityDoc, error) {
				doc["version"] = v

				sent, err := sendEntity(ctx, c, "activate routing strategy "+id, doc,
					func(ctx context.Context, body io.Reader) (*http.Response, error) {
						return c.API().ActionsRoutingStrategyWithBody(ctx, id, contentTypeJSON, body)
					})
				if err != nil {
					return nil, err
				}
				answer = sent
				return nil, nil
			}

			// The body sent to /actions is the action, not the strategy: {name, version}.
			// So whatever get read is discarded here — only its version is carried through,
			// which put stamps on. Under --if-version there is no read at all and this runs
			// on a zero doc.
			activate := func(doc *entityDoc) error {
				*doc = entityDoc{"name": "ACTIVATE"}
				return nil
			}

			if _, err := client.UpdateVersioned(ctx, get, put, activate, version.value()); err != nil {
				return err
			}

			deps.Printer.Notef("Activated routing strategy %s; it is now the live strategy.", id)
			return renderStrategy(deps, answer)
		},
	}

	version.register(cmd.Flags())

	return cmd
}
