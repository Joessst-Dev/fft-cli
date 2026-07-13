package main

import (
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const facilityGetLong = `Show one facility.

<id> is either the platform's UUID or your own tenantFacilityId. fft wraps the
latter as urn:fft:facility:tenantFacilityId:<id> — a form every facility
endpoint accepts — so you can address a facility by the id you gave it:

  fft facility get 0090000042
  fft facility get 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 -o json

-o json prints the API's own JSON, in full. The table is a summary.`

func newFacilityGetCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one facility",
		Long:  facilityGetLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "getFacility"},

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := getFacility(ctx, c, client.FacilityRef(args[0]))
			if err != nil {
				return err
			}
			return renderFacility(deps, raw)
		},
	}
}
