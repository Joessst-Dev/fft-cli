package main

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const routingStrategyListLong = `List the routing strategies.

One of these is 'inUse' — the strategy the engine is routing with right now. The rest
are drafts and superseded revisions, kept so you can read, edit or re-activate them.

  fft routing strategy list
  fft routing strategy list --all -o json | jq -r '.[] | select(.inUse) | .id'

This endpoint pages by id rather than by cursor, so --all fetches every strategy and
stops at --max-items, saying so on stderr if it had to.

stdout carries the strategies and nothing else; the total and every notice go to
stderr.`

func newRoutingStrategyListCmd(deps *Deps) *cobra.Command {
	var page pageFlags

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the routing strategies",
		Long:  routingStrategyListLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "getRoutingStrategies"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			build := func(context.Context, *client.Client) (client.ListOp[json.RawMessage], error) {
				return client.RoutingStrategies(), nil
			}
			return runList(cmd, deps, build, page, routingStrategyList())
		},
	}

	page.register(cmd.Flags(), "strategies", client.DefaultListSize)

	return cmd
}
