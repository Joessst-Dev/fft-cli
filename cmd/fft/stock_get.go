package main

import (
	"github.com/spf13/cobra"
)

const stockGetLong = `Show one stock, by its platform id.

A stock is addressed by the id the platform gave it — unlike a listing, which is
addressed by your own tenantArticleId. 'fft stock list' is where you find the id:

  fft stock list --tenant-article-id 4711 -o json | jq -r '.[].id'
  fft stock get 019c41f1-8f14-7000-9575-25a1b5c8b3a1

-o json prints the API's own JSON, in full. The table is a summary.`

func newStockGetCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <stockId>",
		Short: "Show one stock",
		Long:  stockGetLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "getStock"},

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := getStock(ctx, c, args[0])
			if err != nil {
				return err
			}
			return renderStock(deps, raw)
		},
	}
}
