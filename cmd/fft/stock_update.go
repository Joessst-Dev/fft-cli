package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const stockUpdateLong = `Replace a stock with the contents of a JSON file.

This is a PUT: the stock becomes what the file says and loses anything the file
omits.

  fft stock get 019c41f1-… -o json > stock.json
  $EDITOR stock.json
  fft stock update 019c41f1-… --file stock.json

The API has no If-Match header — optimistic locking travels in the body as a
"version" field — so fft reads the stock first to learn its current version,
sends that version back, and retries once if someone wrote in between. Your file
does not need a version; fft supplies it.

--if-version skips the read: fft sends the version you name and the API answers
409 if it is stale. That is one request instead of two, and a clean failure
instead of a silent overwrite — which is what a CI job wants. (It is
--if-version and never --version: cobra owns --version on the root command.)

To change a quantity across many stocks at once, 'fft stock upsert' is one
request rather than one per stock.`

func newStockUpdateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
		version versionFlag
	)

	cmd := &cobra.Command{
		Use:   "update <stockId> --file <file>",
		Short: "Replace a stock (PUT)",
		Long:  stockUpdateLong,
		// MaximumNArgs and not ExactArgs, because cobra validates the arguments before
		// RunE runs and --example takes no id. The id is demanded below instead, once
		// --example has had its say.
		Args:    usageArgs(cobra.MaximumNArgs(1)),
		Aliases: []string{"replace"},

		Annotations: map[string]string{annotationOperationID: "updateStock"},

		RunE: func(cmd *cobra.Command, args []string) error {
			// --example needs no project, no credentials and no network, so it is
			// answered before anything that does.
			if example {
				return printCommandExample(cmd)
			}
			if len(args) != 1 {
				return exitcode.UsageError{Err: fmt.Errorf(
					"which stock? Name one, or run --example for a body to start from")}
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}
			if err := version.check(); err != nil {
				return err
			}

			raw, err := readBody(deps, file)
			if err != nil {
				return err
			}

			body, err := decodeDoc(raw, file)
			if err != nil {
				return exitcode.UsageError{Err: err}
			}

			id := args[0]

			// A body that names a *different* stock is a mistake, not an instruction: a
			// stock's id is not something a PUT can change, so the user has either edited
			// the wrong file or typed the wrong id — and the API's resolution of the
			// disagreement is not something to leave to chance.
			//
			// The commonest way in is `fft stock get A -o json > s.json` followed by
			// `fft stock update B --file s.json`, which is one keystroke from a write
			// that goes somewhere the user did not look.
			if got := docString(body, "id"); got != "" && got != id {
				return exitcode.UsageError{Err: fmt.Errorf(
					"%s describes stock %s, but you asked to update %s — a stock's id cannot be changed",
					file, got, id)}
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			// The raw answer is captured on the way past, because UpdateVersioned is
			// generic over the decoded entity and -o json must print the bytes the API
			// sent, not fft's re-encoding of them.
			var answer []byte

			get := func(ctx context.Context) (entityDoc, int, error) {
				return getStockDoc(ctx, c, id)
			}

			put := func(ctx context.Context, doc entityDoc, v int) (entityDoc, error) {
				doc["version"] = v

				sent, err := sendEntity(ctx, c, "update stock "+id, doc,
					func(ctx context.Context, body io.Reader) (*http.Response, error) {
						return c.API().UpdateStockWithBody(ctx, id, contentTypeJSON, body)
					})
				if err != nil {
					return nil, err
				}
				answer = sent
				return nil, nil
			}

			// The mutation is a replacement: whatever was read, the file wins. That is
			// what makes this a PUT — and it is why, with --if-version, the file has to be
			// the *whole* stock: there is no read for it to be merged into.
			replace := func(doc *entityDoc) error {
				*doc = body
				return nil
			}

			if _, err := client.UpdateVersioned(ctx, get, put, replace, version.value()); err != nil {
				return err
			}

			deps.Printer.Notef("Updated stock %s.", id)
			return renderStock(deps, answer)
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "JSON file holding the whole stock ('-' for stdin)")
	cmd.Flags().BoolVar(&example, "example", false, "Print a sample request body and exit")
	version.register(cmd.Flags())

	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}
