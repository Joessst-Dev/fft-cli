package main

import (
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const connectionGetLong = `Show one connection of a facility.

<id> is the connection's own id — the one 'fft connection list' prints, and the
one 'fft sourcing simulate' names as the facilityConnectionRef of every transfer
it chose. So an answer to "why did the router send this through Frankfurt?" ends
here, at the edge it used.

  fft connection get 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 --facility BER-01

-o json prints the API's own JSON, in full: the cutoff times, the non-delivery
days, the fallback costs and the context rules the table only counts.`

func newConnectionGetCmd(deps *Deps) *cobra.Command {
	var facility string

	cmd := &cobra.Command{
		Use:   "get <id> --facility <id>",
		Short: "Show one connection",
		Long:  connectionGetLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "getFacilityConnection"},

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

			raw, err := getConnection(ctx, c, client.FacilityRef(facility), args[0])
			if err != nil {
				return err
			}
			return renderConnection(deps, raw)
		},
	}

	registerFacilityFlag(cmd, &facility)

	return cmd
}
