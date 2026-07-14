package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

const sourcingGetLong = `Read a sourcing run back.

<id> is the id 'fft sourcing simulate' reported: the run is kept, so the same
answer can be read again without paying for the routing a second time.

  fft sourcing get 284f32cc-b106-487d-b633-f90d93d8c251
  fft sourcing get 284f32cc-b106-487d-b633-f90d93d8c251 -o json | jq '.result.options[0].transfers'

An option does not stay true forever — each carries a validUntil, and the table
shows it. A run whose options have expired is a record of what the router *would*
have done, not of what it would do now.`

func newSourcingGetCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Read a sourcing run back",
		Long:  sourcingGetLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "getSourcingOption"},

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			res, err := c.Do(ctx, fmt.Sprintf("get sourcing run %s", args[0]),
				func(ctx context.Context) (*http.Response, error) {
					return c.API().GetSourcingOption(ctx, args[0])
				})
			if err != nil {
				return err
			}
			return renderSourcing(deps, res.Body)
		},
	}
}
