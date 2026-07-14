package main

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const connectionListLong = `List the connections that leave a facility.

These are the edges the routing engine may source along. A facility with no
connections cannot fulfil anything to anyone.

  fft connection list --facility BER-01
  fft connection list --facility BER-01 --target FRA-02
  fft connection list --facility BER-01 --all -o json | jq -r '.[].carrierKey'

This endpoint pages by id rather than by cursor, so a page of exactly --size is
always followed by one more request to prove there is nothing after it. --all
does that for you and stops at --max-items, saying so on stderr if it had to.

stdout carries the connections and nothing else. The total, the truncation
notice and every other remark go to stderr.`

func newConnectionListCmd(deps *Deps) *cobra.Command {
	var (
		facility string
		target   string
		page     pageFlags
	)

	cmd := &cobra.Command{
		Use:   "list --facility <id>",
		Short: "List the connections of a facility",
		Long:  connectionListLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "getFacilityConnections"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireFlag(cmd, "facility"); err != nil {
				return err
			}

			build := func(ctx context.Context, c *client.Client) (client.ListOp[json.RawMessage], error) {
				// --facility is a *path* parameter, and path parameters take the URN form —
				// so the tenantFacilityId the user typed needs no lookup and costs no
				// request.
				source := client.FacilityRef(facility)

				// --target is a *query filter*, and query filters do not resolve URNs: the
				// API answers a URN it cannot resolve with a cheerful, empty 200, which
				// reads as "this facility has no connections there" rather than as "you
				// asked the wrong question". So it is resolved to a platform id first, at
				// the cost of one GET. See resolveFacilityID.
				id := ""
				if target != "" {
					var err error
					if id, err = resolveFacilityID(ctx, c, target); err != nil {
						return client.ListOp[json.RawMessage]{}, err
					}
				}

				return client.FacilityConnections(source, id), nil
			}

			return runList(cmd, deps, build, page, connectionList())
		},
	}

	f := cmd.Flags()
	registerFacilityFlag(cmd, &facility)
	f.StringVar(&target, "target", "",
		"Only connections that go to this facility, by tenantFacilityId or platform UUID")
	page.register(f, "connections", client.DefaultListSize)

	return cmd
}
