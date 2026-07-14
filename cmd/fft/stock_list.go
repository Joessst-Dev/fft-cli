package main

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const stockListLong = `List stocks.

This is POST /api/stocks/search with a cursor. The legacy GET /api/stocks is not
used: it has a different page size (default 25, max 100 — do not confuse the
two), and it cannot express these filters.

  fft stock list --facility BER-01
  fft stock list --tenant-article-id 4711 --all
  fft stock list --facility BER-01 -o json | jq '[.[].value] | add'

With no filter you get every stock in the tenant, which on a real tenant is a
lot; --facility or --tenant-article-id is almost always what you want.

VALUE is what is on the shelf, RESERVED is what is already promised to an order,
and AVAILABLE is what is left to sell.`

// stockSortFields are the fields the API will sort stocks by. The search accepts
// exactly one.
var stockSortFields = []string{"tenantArticleId", "value", "locationName", "lastModified"}

func newStockListCmd(deps *Deps) *cobra.Command {
	var (
		facility string
		articles []string
		sortBy   string
		page     pageFlags
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stocks",
		Long:  stockListLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "searchStock"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := client.StockSearchPayload{Query: stockQuery(facility, articles)}

			var err error
			if payload.Sort, err = stockSort(sortBy); err != nil {
				return err
			}

			return runSearch(cmd, deps, client.StockSearch[json.RawMessage](), staticQuery(payload), page, stockList())
		},
	}

	f := cmd.Flags()
	f.StringVar(&facility, "facility", "", "Only the stocks of this facility, by tenantFacilityId or platform UUID")
	f.StringSliceVar(&articles, "tenant-article-id", nil, "Only the stocks of these articles")
	f.StringVar(&sortBy, "sort", "",
		"Sort by one field, as field:asc or field:desc ("+strings.Join(stockSortFields, ", ")+")")
	page.register(f, "stocks", client.DefaultSize)

	return cmd
}

// stockQuery turns the filter flags into the API's query.
//
// The facility filter has two spellings and they are not interchangeable: a
// platform UUID is matched against facilityRef, the tenant's own id against
// tenantFacilityId. Sending one where the other belongs is a filter that silently
// matches nothing — a 200 with an empty list, which reads as "there is no stock
// here" rather than as "you asked the wrong question".
func stockQuery(facility string, articles []string) api.StockSearchQuery {
	var query api.StockSearchQuery

	switch key, value := client.FacilitySelector(facility); key {
	case client.KeyFacilityRef:
		query.FacilityRef = &api.StringFilter{Eq: ptr(value)}
	case client.KeyTenantFacilityID:
		query.TenantFacilityId = &api.StringFilter{Eq: ptr(value)}
	}

	if len(articles) > 0 {
		query.TenantArticleId = &api.StringFilter{In: &articles}
	}

	return query
}

// stockSort parses --sort into the API's sort object.
func stockSort(v string) ([]api.StockSort, error) {
	return parseSort(v, stockSortFields, func(s *api.StockSort, field, dir string) bool {
		switch field {
		case "tenantarticleid":
			s.TenantArticleId = ptr(api.StockSortTenantArticleId(dir))
		case "value":
			s.Value = ptr(api.StockSortValue(dir))
		case "locationname":
			s.LocationName = ptr(api.StockSortLocationName(dir))
		case "lastmodified":
			s.LastModified = ptr(api.StockSortLastModified(dir))
		default:
			return false
		}
		return true
	})
}
