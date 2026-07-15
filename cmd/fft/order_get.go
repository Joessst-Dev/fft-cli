package main

import (
	"github.com/spf13/cobra"
)

const orderGetLong = `Show one order.

<id> is the order's platform id, the one 'fft order list' and 'fft order search'
print. Unlike a facility, an order has no tenantFacilityId-style shorthand.

  fft order get 8f14e45f-ceea-467a-9575-25a1b5c8b3a1
  fft order get 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 -o json | jq .status

-o json prints the API's own JSON, in full. The table is a summary.`

func newOrderGetCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one order",
		Long:  orderGetLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "getOrder"},

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := getOrder(ctx, c, args[0])
			if err != nil {
				return err
			}
			return renderOrder(deps, raw)
		},
	}
}
