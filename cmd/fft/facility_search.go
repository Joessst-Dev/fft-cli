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
	return newFileSearchCmd[api.FacilitySearchQuery, api.FacilitySort](deps, fileSearch{
		short:       "Search facilities with a JSON query",
		long:        facilitySearchLong,
		operationID: "searchFacility",
		entity:      "facility",
		op:          client.FacilitySearch[json.RawMessage](),
		view:        facilityList(),
	})
}
