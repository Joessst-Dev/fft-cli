package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

const stockDeleteLong = `Delete one stock.

The quantity is removed; the article's listing at that facility is untouched, and
so is its stock at other facilities and other locations.

  fft stock delete 019c41f1-8f14-7000-9575-25a1b5c8b3a1

fft asks first; -y/--yes answers for you. On a non-interactive terminal there is
nobody to ask, and fft refuses rather than assuming yes.

To delete many stocks at once — every stock at a location, or every stock of a
set of articles — use 'fft stock actions', which does it in one request.`

func newStockDeleteCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <stockId>",
		Short:   "Delete one stock",
		Long:    stockDeleteLong,
		Args:    usageArgs(cobra.ExactArgs(1)),
		Aliases: []string{"rm"},

		Annotations: map[string]string{annotationOperationID: "deleteStock"},

		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			ok, err := confirmDestructive(deps, fmt.Sprintf("Delete stock %s? This cannot be undone.", id))
			if err != nil {
				return err
			}
			if !ok {
				deps.Printer.Notef("Aborted; stock %s was not deleted.", id)
				return nil
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			// A DELETE is idempotent, so Do may safely retry it on a dropped connection.
			if _, err := c.Do(ctx, "delete stock "+id, func(ctx context.Context) (*http.Response, error) {
				return c.API().DeleteStock(ctx, id)
			}); err != nil {
				return err
			}

			// Nothing on stdout: a delete has no data to emit, and printing a cheerful
			// sentence there would land in the pipe of anyone who scripted it.
			deps.Printer.Notef("Deleted stock %s.", id)
			return nil
		},
	}
}
