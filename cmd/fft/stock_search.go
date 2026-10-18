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
	return newFileSearchCmd[api.StockSearchQuery, api.StockSort](deps, fileSearch{
		short:       "Search stocks with a JSON query",
		long:        stockSearchLong,
		operationID: "searchStock",
		entity:      "stock",
		op:          client.StockSearch[json.RawMessage](),
		view:        stockList(),
	})
}
