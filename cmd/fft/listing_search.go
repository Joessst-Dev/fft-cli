package main

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const listingSearchLong = `Search listings tenant-wide with a query from a JSON file.

'fft listing list' covers the common filters, but is scoped to one facility.
This is POST /api/listings/search unfiltered: it spans every facility, and it
reaches the filters the flags do not — price ranges, tags, category refs, custom
attributes, and and/or trees.

  {
    "query": {
      "and": [
        { "status": { "eq": "ACTIVE" } },
        { "price": { "gte": 100 } }
      ]
    },
    "sort": [ { "tenantArticleId": "ASC" } ]
  }

fft checks the query against the API's schema before sending it, so a misspelled
field is a message that names the field rather than a 200 that quietly did not
filter.

--size, --total and --all override whatever the file said.`

func newListingSearchCmd(deps *Deps) *cobra.Command {
	return newFileSearchCmd[api.ListingSearchQuery, api.ListingSort](deps, fileSearch{
		short:       "Search listings with a JSON query",
		long:        listingSearchLong,
		operationID: "searchListing",
		entity:      "listing",
		op:          client.ListingSearch[json.RawMessage](),
		view:        listingList(),
	})
}
