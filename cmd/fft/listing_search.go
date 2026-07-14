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
	var (
		file    string
		example bool
		page    pageFlags
	)

	cmd := &cobra.Command{
		Use:   "search --file <file>",
		Short: "Search listings with a JSON query",
		Long:  listingSearchLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "searchListing"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example needs no project, no credentials and no network, so it is
			// answered before anything that does.
			if example {
				return printCommandExample(cmd)
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}

			payload, err := searchPayload[api.ListingSearchQuery, api.ListingSort](deps, file, "listing")
			if err != nil {
				return err
			}

			return runSearch(cmd, deps, client.ListingSearch[json.RawMessage](), staticQuery(payload), page, listingList())
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the search payload ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	page.register(f, "listings", client.DefaultSize)

	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}
