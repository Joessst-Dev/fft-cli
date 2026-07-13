package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

const listingDeleteLong = `Delete ONE listing from a facility.

This removes the article from that facility's catalog. It does not touch the
article's listings at other facilities, and it does not touch its stock.

To delete every listing of a facility, there is a separate command — 'fft
listing purge'. It is separate on purpose: a destructive verb should not be one
forgotten argument away from a merely careless one.

  fft listing delete --facility BER-01 4711

fft asks first; -y/--yes answers for you. On a non-interactive terminal there is
nobody to ask, and fft refuses rather than assuming yes.`

func newListingDeleteCmd(deps *Deps) *cobra.Command {
	var facility string

	cmd := &cobra.Command{
		Use:     "delete --facility <id> <tenantArticleId>",
		Short:   "Delete one listing from a facility",
		Long:    listingDeleteLong,
		Args:    usageArgs(cobra.ExactArgs(1)),
		Aliases: []string{"rm"},

		Annotations: map[string]string{annotationOperationID: "deleteFacilityListing"},

		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := requireFacility(cmd, facility)
			if err != nil {
				return err
			}
			article := args[0]

			ok, err := confirmDestructive(deps, fmt.Sprintf(
				"Delete listing %s from facility %s? This cannot be undone.", article, ref))
			if err != nil {
				return err
			}
			if !ok {
				deps.Printer.Notef("Aborted; listing %s was not deleted.", article)
				return nil
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			op := fmt.Sprintf("delete listing %s of facility %s", article, ref)

			// A DELETE is idempotent, so Do may safely retry it on a dropped connection.
			if _, err := c.Do(ctx, op, func(ctx context.Context) (*http.Response, error) {
				return c.API().DeleteFacilityListing(ctx, ref, article)
			}); err != nil {
				return err
			}

			// Nothing on stdout: a delete has no data to emit, and printing a cheerful
			// sentence there would land in the pipe of anyone who scripted it.
			deps.Printer.Notef("Deleted listing %s from facility %s.", article, ref)
			return nil
		},
	}

	cmd.Flags().StringVar(&facility, "facility", "", "The facility, by tenantFacilityId or platform UUID (required)")

	return cmd
}
