package main

import (
	"context"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

const routingStrategyUpdateLong = `Replace a routing strategy with the contents of a JSON file.

This is a PUT, and there is no PATCH: the strategy becomes what the file says and
loses anything the file omits. The body must carry the whole strategy — name, root
node and global configuration — so the way to change one fence is to read the whole
strategy, edit it, and send it back:

  fft routing strategy get 3f9c... -o json > s.json
  $EDITOR s.json
  fft routing strategy update 3f9c... --file s.json

fft supplies the version. The API locks optimistically and carries the version in the
body rather than in a header, so fft reads the strategy first to learn the current
one and retries once if somebody wrote in between.

--if-version skips the read: fft sends the version you name and the API rejects it if
it is stale — one request instead of two, and a clean 409 instead of a silent
overwrite.`

func newRoutingStrategyUpdateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		version versionFlag
	)

	cmd := &cobra.Command{
		Use:     "update <id> --file <file>",
		Short:   "Replace a routing strategy (PUT)",
		Long:    routingStrategyUpdateLong,
		Args:    usageArgs(cobra.ExactArgs(1)),
		Aliases: []string{"replace"},

		Annotations: map[string]string{annotationOperationID: "putRoutingStrategy"},

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
				return getStrategyDoc(ctx, c, id)
			}

			// A PUT: whatever was read, the file wins. Which is why --if-version demands
			// the whole strategy in the file — there is no read for a partial one to be
			// merged into.
			answer, err := putVersioned(ctx, c, "update routing strategy "+id, get, replaceWith(body), version.value(),
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().PutRoutingStrategyWithBody(ctx, id, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			deps.Printer.Notef("Updated routing strategy %s.", id)
			return renderStrategy(deps, answer)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the whole strategy ('-' for stdin)")
	version.register(f)

	return cmd
}
