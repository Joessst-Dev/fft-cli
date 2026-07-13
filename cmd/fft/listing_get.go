package main

import (
	"github.com/spf13/cobra"
)

const listingGetLong = `Show one listing.

A listing is addressed by the tenantArticleId you gave the article — not by a
platform UUID. That is unlike every other entity in this API, and it is why the
article id is the positional argument while the facility is a flag:

  fft listing get --facility BER-01 4711
  fft listing get --facility BER-01 4711 -o json | jq .price

-o json prints the API's own JSON, in full. The table is a summary.`

func newListingGetCmd(deps *Deps) *cobra.Command {
	var facility string

	cmd := &cobra.Command{
		Use:   "get --facility <id> <tenantArticleId>",
		Short: "Show one listing",
		Long:  listingGetLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "getListing"},

		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := requireFacility(cmd, facility)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := getListing(ctx, c, ref, args[0])
			if err != nil {
				return err
			}
			return renderListing(deps, raw)
		},
	}

	cmd.Flags().StringVar(&facility, "facility", "", "The facility, by tenantFacilityId or platform UUID (required)")

	return cmd
}
