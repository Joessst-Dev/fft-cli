package main

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const stockSearchLong = `Search stocks with a query from a JSON file.

'fft stock list' covers the common filters. This is for the ones it does not:
value ranges, receipt and expiry dates, scannable codes, storage-location traits,
stock properties, custom attributes, and and/or trees.

  {
    "query": {
      "and": [
        { "facilityRef": { "eq": "8f14e45f-ceea-467a-9575-25a1b5c8b3a1" } },
        { "value": { "gt": 0 } }
      ]
    },
    "sort": [ { "value": "DESC" } ]
  }

fft checks the query against the API's schema before sending it, so a misspelled
field is a message that names the field rather than a 200 that quietly did not
filter.

--size, --total and --all override whatever the file said.`

func newStockSearchCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
		page    pageFlags
	)

	cmd := &cobra.Command{
		Use:   "search --file <file>",
		Short: "Search stocks with a JSON query",
		Long:  stockSearchLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "searchStock"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example needs no project, no credentials and no network, so it is
			// answered before anything that does.
			if example {
				return printCommandExample(cmd)
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}

			payload, err := searchPayload[api.StockSearchQuery, api.StockSort](deps, file, "stock")
			if err != nil {
				return err
			}

			return runSearch(cmd, deps, client.StockSearch[json.RawMessage](), staticQuery(payload), page, stockList())
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the search payload ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	page.register(f, "stocks")

	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}
