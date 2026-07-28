package main

import (
	"github.com/spf13/cobra"
)

const routingStrategyGetLong = `Show one routing strategy.

  fft routing strategy get 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10

The table is a one-line summary — name, revision, whether it is live, version. -o json
prints the whole thing: the root node, every fence and rating, the conditions that
branch to further nodes, and the global configuration. That JSON is also what 'update'
expects back, so this is where a round-trip edit starts.`

func newRoutingStrategyGetCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one routing strategy",
		Long:  routingStrategyGetLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "getRoutingStrategy"},

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := getStrategy(ctx, c, args[0])
			if err != nil {
				return err
			}
			return renderStrategy(deps, raw)
		},
	}
}
