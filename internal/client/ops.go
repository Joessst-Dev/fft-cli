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

// OrderSearchPayload is the body of POST /api/orders/search.
type OrderSearchPayload = SearchPayload[api.OrderSearchQuery, api.OrderSort]

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

// OrderSearch is POST /api/orders/search, decoding each order into T.
func OrderSearch[T any]() Op[T] {
	return Op[T]{
		Name:  "search the orders",
		Items: "orders",
		Do: func(ctx context.Context, raw api.ClientInterface, body []byte) (*http.Response, error) {
			return raw.SearchOrderWithBody(ctx, contentTypeJSON, bytes.NewReader(body))
		},
	}
}

// Orders is GET /api/orders, decoding each order into json.RawMessage.
//
// It is a [ListOp] and not an [Op] because the GET list pages by startAfterId, not
// by a cursor — POST /api/orders/search is the cursor search (see [OrderSearch]).
//
// tenantOrderID and consumerID are the only two filters the GET list offers, both
// exact-match query params; they are closed over here the way [FacilityConnections]
// closes over its target. Anything richer (status, date range) is search-only.
func Orders(tenantOrderID, consumerID string) ListOp[json.RawMessage] {
	return ListOp[json.RawMessage]{
		Name:  "list the orders",
		Items: "orders",
		ID:    RawID,
		Do: func(ctx context.Context, raw api.ClientInterface, after string, size int) (*http.Response, error) {
			params := &api.GetAllOrdersParams{}
			if tenantOrderID != "" {
				params.TenantOrderId = &tenantOrderID
			}
			if consumerID != "" {
				params.ConsumerId = &consumerID
			}
			if after != "" {
				params.StartAfterId = &after
			}
			if size != 0 {
				params.Size = &size
			}
			return raw.GetAllOrders(ctx, params)
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

// RoutingStrategies is GET /api/routing/strategies, decoding each strategy into
// json.RawMessage.
//
// It is a [ListOp] and not an [Op] because the strategies have no /search: they page
// by startAfterId, and their envelope is {routingStrategies, total}. See the top of
// list.go.
func RoutingStrategies() ListOp[json.RawMessage] {
	return ListOp[json.RawMessage]{
		Name:  "list the routing strategies",
		Items: "routingStrategies",
		ID:    RawID,
		Do: func(ctx context.Context, raw api.ClientInterface, after string, size int) (*http.Response, error) {
			params := &api.GetRoutingStrategiesParams{}
			if after != "" {
				params.StartAfterId = &after
			}
			if size != 0 {
				params.Size = &size
			}
			return raw.GetRoutingStrategies(ctx, params)
		},
	}
}

// RoutingCategories is GET /api/routing/nodeconfigcategories, decoding each node
// config category into json.RawMessage. Its envelope array is
// routingStrategyNodeConfigCategories.
func RoutingCategories() ListOp[json.RawMessage] {
	return ListOp[json.RawMessage]{
		Name:  "list the routing node config categories",
		Items: "routingStrategyNodeConfigCategories",
		ID:    RawID,
		Do: func(ctx context.Context, raw api.ClientInterface, after string, size int) (*http.Response, error) {
			params := &api.GetRoutingStrategyNodeConfigCategoriesParams{}
			if after != "" {
				params.StartAfterId = &after
			}
			if size != 0 {
				params.Size = &size
			}
			return raw.GetRoutingStrategyNodeConfigCategories(ctx, params)
		},
	}
}

// RoutingDecisionLogsFilter is the exact-match filter set of GET
// /api/routing/decisionlogs. Every field is an id the API compares literally; an empty
// one is simply not sent, exactly as the two order filters behave on [Orders].
type RoutingDecisionLogsFilter struct {
	OrderRef           string
	RoutingPlanRef     string
	ProcessRef         string
	TenantOrderID      string
	SourcingOptionRef  string
	SourcingOptionsRef string
}

// RoutingDecisionLogs is GET /api/routing/decisionlogs, decoding each decision log into
// json.RawMessage. Its envelope array is decisionLogs.
//
// The endpoint offers only exact-match id filters — closed over here the way
// [FacilityConnections] closes over its target — and pages by startAfterId.
func RoutingDecisionLogs(filter RoutingDecisionLogsFilter) ListOp[json.RawMessage] {
	return ListOp[json.RawMessage]{
		Name:  "list the routing decision logs",
		Items: "decisionLogs",
		ID:    RawID,
		Do: func(ctx context.Context, raw api.ClientInterface, after string, size int) (*http.Response, error) {
			params := &api.GetRoutingDecisionLogsParams{}
			if filter.OrderRef != "" {
				params.OrderRef = &filter.OrderRef
			}
			if filter.RoutingPlanRef != "" {
				params.RoutingPlanRef = &filter.RoutingPlanRef
			}
			if filter.ProcessRef != "" {
				params.ProcessRef = &filter.ProcessRef
			}
			if filter.TenantOrderID != "" {
				params.TenantOrderId = &filter.TenantOrderID
			}
			if filter.SourcingOptionRef != "" {
				params.SourcingOptionRef = &filter.SourcingOptionRef
			}
			if filter.SourcingOptionsRef != "" {
				params.SourcingOptionsRef = &filter.SourcingOptionsRef
			}
			if after != "" {
				params.StartAfterId = &after
			}
			if size != 0 {
				params.Size = &size
			}
			return raw.GetRoutingDecisionLogs(ctx, params)
		},
	}
}
