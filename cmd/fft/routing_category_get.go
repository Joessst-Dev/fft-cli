package main

import (
	"github.com/spf13/cobra"
)

const routingCategoryGetLong = `Show one routing node config category.

  fft routing category get 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10

-o json prints the whole category, which is also what 'update' expects back.`

func newRoutingCategoryGetCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one routing category",
		Long:  routingCategoryGetLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "getRoutingStrategyNodeConfigCategory"},

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := getCategory(ctx, c, args[0])
			if err != nil {
				return err
			}
			return renderCategory(deps, raw)
		},
	}
}
