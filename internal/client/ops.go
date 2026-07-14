package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/Joessst-Dev/fft-cli/internal/api"
)

// The searchable entities. Each is four lines, because [SearchPayload] and
// [Search] already know everything about a cursor search that is not the path and
// the name of the array in the answer.
//
// T is the caller's: a command that only renders a name and a status decodes into
// a view struct and never touches the generated union; one that needs the whole
// entity decodes into the generated model. The wire format is the same either way.

// FacilitySearchPayload is the body of POST /api/facilities/search.
type FacilitySearchPayload = SearchPayload[api.FacilitySearchQuery, api.FacilitySort]

// ListingSearchPayload is the body of POST /api/listings/search.
type ListingSearchPayload = SearchPayload[api.ListingSearchQuery, api.ListingSort]

// StockSearchPayload is the body of POST /api/stocks/search.
type StockSearchPayload = SearchPayload[api.StockSearchQuery, api.StockSort]

// FacilitySearch is POST /api/facilities/search, decoding each facility into T.
func FacilitySearch[T any]() Op[T] {
	return Op[T]{
		Name:  "search the facilities",
		Items: "facilities",
		Do: func(ctx context.Context, raw api.ClientInterface, body []byte) (*http.Response, error) {
			return raw.SearchFacilityWithBody(ctx, contentTypeJSON, bytes.NewReader(body))
		},
	}
}

// ListingSearch is POST /api/listings/search, decoding each listing into T.
func ListingSearch[T any]() Op[T] {
	return Op[T]{
		Name:  "search the listings",
		Items: "listings",
		Do: func(ctx context.Context, raw api.ClientInterface, body []byte) (*http.Response, error) {
			return raw.SearchListingWithBody(ctx, contentTypeJSON, bytes.NewReader(body))
		},
	}
}

// StockSearch is POST /api/stocks/search, decoding each stock into T.
func StockSearch[T any]() Op[T] {
	return Op[T]{
		Name:  "search the stocks",
		Items: "stocks",
		Do: func(ctx context.Context, raw api.ClientInterface, body []byte) (*http.Response, error) {
			return raw.SearchStockWithBody(ctx, contentTypeJSON, bytes.NewReader(body))
		},
	}
}

// FacilityConnections is GET /api/facilities/{facilityId}/connections, decoding each
// connection into json.RawMessage.
//
// It is a [ListOp] and not an [Op] because the connections have no /search: they page
// by startAfterId. See the top of list.go.
//
// facilityID goes in the path, so it may be a URN — [FacilityRef] is enough. target
// goes in a *query* filter, and query filters do not resolve URNs: it must already be
// a platform id, or the API answers a cheerful, empty 200. See resolveFacilityID in
// cmd/fft/facility.go, which is what the caller uses to get one.
func FacilityConnections(facilityID, target string) ListOp[json.RawMessage] {
	return ListOp[json.RawMessage]{
		Name:  "list the facility's connections",
		Items: "interFacilityConnections",
		ID:    RawID,
		Do: func(ctx context.Context, raw api.ClientInterface, after string, size int) (*http.Response, error) {
			params := &api.GetFacilityConnectionsParams{}
			if target != "" {
				params.TargetFacilityRef = &target
			}
			if after != "" {
				params.StartAfterId = &after
			}
			if size != 0 {
				params.Size = &size
			}
			return raw.GetFacilityConnections(ctx, facilityID, params)
		},
	}
}
