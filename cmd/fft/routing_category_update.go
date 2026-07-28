package main

import (
	"context"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const routingCategoryUpdateLong = `Replace a routing node config category (PUT).

The category becomes what the file says; there is no PATCH. Read it, edit it, send it
back:

  fft routing category get 3f9c... -o json > c.json
  $EDITOR c.json
  fft routing category update 3f9c... --file c.json

fft supplies the version: it reads the category first to learn the current one and
retries once on a conflict. --if-version skips that read and fails cleanly with a 409
if the version you name is stale.`

func newRoutingCategoryUpdateCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		version versionFlag
	)

	cmd := &cobra.Command{
		Use:     "update <id> --file <file>",
		Short:   "Replace a routing category (PUT)",
		Long:    routingCategoryUpdateLong,
		Args:    usageArgs(cobra.ExactArgs(1)),
		Aliases: []string{"replace"},

		Annotations: map[string]string{annotationOperationID: "putRoutingStrategyNodeConfigCategory"},

		RunE: func(cmd *cobra.Command, args []string) error {
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

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			id := args[0]
			var answer []byte

			get := func(ctx context.Context) (entityDoc, int, error) {
				return getCategoryDoc(ctx, c, id)
			}

			put := func(ctx context.Context, doc entityDoc, v int) (entityDoc, error) {
				doc["version"] = v

				sent, err := sendEntity(ctx, c, "update routing category "+id, doc,
					func(ctx context.Context, body io.Reader) (*http.Response, error) {
						return c.API().PutRoutingStrategyNodeConfigCategoryWithBody(ctx, id, contentTypeJSON, body)
					})
				if err != nil {
					return nil, err
				}
				answer = sent
				return nil, nil
			}

			replace := func(doc *entityDoc) error {
				*doc = body
				return nil
			}

			if _, err := client.UpdateVersioned(ctx, get, put, replace, version.value()); err != nil {
				return err
			}

			deps.Printer.Notef("Updated routing category %s.", id)
			return renderCategory(deps, answer)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the whole category ('-' for stdin)")
	version.register(f)

	return cmd
}
