package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const listingListLong = `List the listings of one facility.

This is POST /api/listings/search filtered to the facility, not the legacy
GET /api/facilities/{id}/listings: the search API returns whole listings and
pages on a cursor, while the legacy list returns a reduced projection.

  fft listing list --facility BER-01
  fft listing list --facility BER-01 --status ACTIVE --all
  fft listing list --facility BER-01 -o json | jq -r '.[].tenantArticleId'

--facility takes your own tenantFacilityId or the platform's UUID.

Remember what a listing is: the catalog entry, not the quantity. For quantities,
'fft stock list --facility BER-01'.`

// listingSortFields are the fields the API will sort listings by. There are only
// these two, and the search accepts exactly one.
var listingSortFields = []string{"tenantArticleId", "lastModified"}

func newListingListCmd(deps *Deps) *cobra.Command {
	var (
		facility string
		status   []string
		articles []string
		sortBy   string
		page     pageFlags
	)

	cmd := &cobra.Command{
		Use:   "list --facility <id>",
		Short: "List the listings of a facility",
		Long:  listingListLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "searchListing"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireFlag(cmd, "facility"); err != nil {
				return err
			}

			sort, err := listingSort(sortBy)
			if err != nil {
				return err
			}

			// The facility is resolved inside the build, because resolving it takes a
			// request and therefore a client — see [listingFacilityQuery] for why the URN
			// form will not do.
			build := func(ctx context.Context, c *client.Client) (client.ListingSearchPayload, error) {
				var payload client.ListingSearchPayload

				id, err := resolveFacilityID(ctx, c, facility)
				if err != nil {
					return payload, err
				}

				query, err := listingQuery(id, status, articles)
				if err != nil {
					return payload, err
				}
				return client.ListingSearchPayload{Query: query, Sort: sort}, nil
			}

			return runSearch(cmd, deps, client.ListingSearch[json.RawMessage](), build, page, listingList())
		},
	}

	f := cmd.Flags()
	f.StringVar(&facility, "facility", "", "The facility, by tenantFacilityId or platform UUID (required)")
	f.StringSliceVar(&status, "status", nil,
		"Only listings in these states: "+strings.Join(listingStatuses(), ", "))
	f.StringSliceVar(&articles, "tenant-article-id", nil, "Only the listings of these articles")
	f.StringVar(&sortBy, "sort", "",
		"Sort by one field, as field:asc or field:desc ("+strings.Join(listingSortFields, ", ")+")")
	page.register(f, "listings", client.DefaultSize)

	registerEnumCompletion(cmd, "status", listingStatuses())

	return cmd
}

// listingQuery turns the filter flags into the API's query.
//
// The facility is a *filter*, not a path parameter, because the search endpoint is
// tenant-wide — and so facilityID must already be the platform id. See
// [listingFacilityQuery].
func listingQuery(facilityID string, status, articles []string) (api.ListingSearchQuery, error) {
	query := listingFacilityQuery(facilityID)

	if len(status) > 0 {
		in := make([]api.ListingStatusEnumEnumFilterIn, 0, len(status))
		for _, s := range status {
			v, err := enumValue("status", s, listingStatuses())
			if err != nil {
				return query, err
			}
			in = append(in, api.ListingStatusEnumEnumFilterIn(v))
		}
		query.Status = &api.ListingStatusEnumEnumFilter{In: &in}
	}

	if len(articles) > 0 {
		query.TenantArticleId = &api.StringFilter{In: &articles}
	}

	return query, nil
}

// listingFacilityQuery scopes a listing search to one facility.
//
// facilityID must be the facility's *platform id*, not the URN form of a
// tenantFacilityId. api.ListingSearchQuery has no tenantFacilityId field — only
// facilityRef — and the search index does not resolve a URN in it: it matches
// nothing and answers 200. Confirmed against the live tenant (2026-07-12), where
// facility 0090000020 has 760 listings and the URN filter returned **0**. So every
// caller resolves first, with [resolveFacilityID].
//
// `fft listing purge` uses this too, to count what it is about to destroy. A purge
// that counted a *different* set from the one it deletes would be the worst bug in
// this command group — and before the resolve, it was one: purge would have
// reported "no listings; nothing to purge" for a facility holding 760.
func listingFacilityQuery(facilityID string) api.ListingSearchQuery {
	var query api.ListingSearchQuery
	if facilityID != "" {
		query.FacilityRef = &api.StringFilter{Eq: ptr(facilityID)}
	}
	return query
}

// listingSort parses --sort into the API's sort object.
func listingSort(v string) ([]api.ListingSort, error) {
	return parseSort(v, listingSortFields, func(s *api.ListingSort, field, dir string) bool {
		switch field {
		case "tenantarticleid":
			s.TenantArticleId = ptr(api.ListingSortTenantArticleId(dir))
		case "lastmodified":
			s.LastModified = ptr(api.ListingSortLastModified(dir))
		default:
			return false
		}
		return true
	})
}
