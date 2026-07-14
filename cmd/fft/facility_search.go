package main

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const facilitySearchLong = `Search facilities with a query from a JSON file.

'fft facility list' covers the common filters. This is for the ones it does not:
nested address and contact filters, custom attributes, regex on the name, and
and/or trees.

The file holds the request body of POST /api/facilities/search — the query, and
optionally sort, size and options:

  {
    "query": {
      "and": [
        { "status": { "eq": "ONLINE" } },
        { "address": { "city": { "eq": "Berlin" } } }
      ]
    },
    "sort": [ { "name": "ASC" } ]
  }

fft checks the query against the API's schema before sending it, so a misspelled
field is a message that names the field rather than a 400 that does not.

--size, --total and --all override whatever the file said.`

func newFacilitySearchCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
		page    pageFlags
	)

	cmd := &cobra.Command{
		Use:   "search --file <file>",
		Short: "Search facilities with a JSON query",
		Long:  facilitySearchLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "searchFacility"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example needs no project, no credentials and no network, so it is
			// answered before anything that does.
			if example {
				return printCommandExample(cmd)
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}

			payload, err := searchPayload[api.FacilitySearchQuery, api.FacilitySort](deps, file, "facility")
			if err != nil {
				return err
			}

			return runSearch(cmd, deps, client.FacilitySearch[json.RawMessage](), staticQuery(payload), page, facilityList())
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON file holding the search payload ('-' for stdin)")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	page.register(f, "facilities", client.DefaultSize)

	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}
