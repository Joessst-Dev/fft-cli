package main

import (
	"context"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

const orderUpdateLong = `Update an order from a JSON file.

This is a PATCH with OrderForUpdate: the fields you include are changed and the
rest are left alone — with one trap. orderLineItems is a *full replacement*, not a
merge: if you send it, only the lines you include remain, and any you omit are
deleted. To change one line, send them all.

The API has no If-Match header — optimistic locking travels in the body as a
"version" field — so fft reads the order first to learn its current version, sends
that version back, and retries once if someone wrote in between. Your file does
not need a version; fft supplies it.

  fft order get 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 -o json > order.json
  $EDITOR order.json
  fft order update 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 --file order.json

--if-version skips the read: fft sends the version you name and the API answers
409 if it is stale. That is one request instead of two, and a clean failure
instead of a silent overwrite. (It is --if-version and never --version: cobra owns
--version on the root command.)`

func newOrderUpdateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		version versionFlag
	)

	cmd := &cobra.Command{
		Use:   "update <id> --file <file>",
		Short: "Update an order (PATCH)",
		Long:  orderUpdateLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "updateOrder"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}
			if err := version.check(); err != nil {
				return err
			}

			body, err := readDoc(deps, file)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			id := args[0]

			get := func(ctx context.Context) (entityDoc, int, error) {
				return getOrderDoc(ctx, c, id)
			}

			// The mutation is the file: whatever was read, the file's fields are what
			// gets sent. fft adds only the version. This is why, with --if-version, the
			// file has to carry everything the update should change — there is no read
			// for it to be merged into.
			answer, err := putVersioned(ctx, c, "update order "+id, get, replaceWith(body), version.value(),
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().UpdateOrderWithBody(ctx, id, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			deps.Printer.Notef("Updated order %s.", id)
			return renderOrder(deps, answer)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the order changes ('-' for stdin)")
	version.register(f)

	return cmd
}
