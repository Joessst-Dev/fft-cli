package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const orderSearchLong = `Search the orders of your tenant.

This is POST /api/orders/search: unlike 'fft order list' it returns whole orders
and filters on status, a date range and a sort. It is guarded by the ORDER_READ
permission — the same permission 'fft order list' needs.

A narrow --since/--until is strongly recommended: the API pages far faster when
the orderDate range is bounded.

On a tenant with a very large order history the API can refuse a --since/--until
range or a --sort with HTTP 400 "Query too complex", and a smaller --size does not
help. The spec offers no other filter or endpoint for "the most recent orders":
'fft order list' (GET /api/orders) filters only on tenantOrderId and consumerId,
and an unsorted or --status-only search returns the oldest orders first. What does
work there is a narrow filter — --tenant-order-id — or, to see what the platform
routed most recently, 'fft routing decision-logs', which lists the newest routings
first. fft prints that hint when it sees this 400; the exit code stays 1.

  fft order search --status OPEN
  fft order search --status LOCKED --status PROMISED --sort orderDate:desc
  fft order search --since 2026-07-01 --until 2026-07-15 --all -o json | jq -r '.[].id'

--since and --until are dates (2026-07-01) or full timestamps
(2026-07-01T00:00:00Z). stdout carries the orders and nothing else; the total and
the truncation notice go to stderr.`

// orderSortFields are the fields the search API will sort by. It accepts exactly
// one — not zero, and not two.
var orderSortFields = []string{"orderDate", "status"}

func newOrderSearchCmd(deps *Deps) *cobra.Command {
	var (
		status   []string
		tenantID string
		since    string
		until    string
		sortBy   string
		page     pageFlags
	)

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search orders",
		Long:  orderSearchLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "searchOrder"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			query, err := orderQuery(status, tenantID, since, until)
			if err != nil {
				return err
			}

			payload := client.OrderSearchPayload{Query: query}
			if payload.Sort, err = orderSort(sortBy); err != nil {
				return err
			}

			err = runSearch(cmd, deps, client.OrderSearch[json.RawMessage](), staticQuery(payload), page, orderList())
			return explainQueryTooComplex(err)
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&status, "status", nil,
		"Only orders in these states: "+strings.Join(orderStatuses(), ", "))
	f.StringVar(&tenantID, "tenant-order-id", "", "Only the order with this tenantOrderId")
	f.StringVar(&since, "since", "", "Only orders whose orderDate is on or after this date")
	f.StringVar(&until, "until", "", "Only orders whose orderDate is on or before this date")
	f.StringVar(&sortBy, "sort", "",
		"Sort by one field, as field:asc or field:desc ("+strings.Join(orderSortFields, ", ")+")")
	page.register(f, "orders", client.DefaultSize)

	registerEnumCompletion(cmd, "status", orderStatuses())

	return cmd
}

// queryTooComplexError is the API's "Query too complex" 400 plus the way out. It
// wraps rather than replaces the API error, so the message is the API's own words
// and the exit code is still the one the status maps to.
type queryTooComplexError struct{ err *client.APIError }

func (e *queryTooComplexError) Error() string { return e.err.Error() }
func (e *queryTooComplexError) Unwrap() error { return e.err }

// Hint corrects the API's advice: shrinking --size is what the message suggests
// and what does not work, because the cost is in the date range or sort over the
// whole history, not in the page.
func (e *queryTooComplexError) Hint() string {
	return "A smaller --size will not help: the API rejects --since/--until and --sort on a very large order history.\n" +
		"Narrow the search instead: 'fft order search --tenant-order-id <id>', or 'fft order list --tenant-order-id <id>'.\n" +
		"To see what was routed most recently, 'fft routing decision-logs' lists the newest routings first."
}

// explainQueryTooComplex attaches the hint to the API's "Query too complex" 400
// and passes every other error through untouched. It matches the message, because
// the status alone is the same 400 an unknown enum value gets.
func explainQueryTooComplex(err error) error {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		return err
	}
	if !strings.Contains(strings.ToLower(apiErr.Error()), "query too complex") {
		return err
	}
	return &queryTooComplexError{err: apiErr}
}

// orderQuery turns the filter flags into the API's query. Every value is checked
// here, because the API's own answer to an unknown status or an unparseable date
// is a 400 that does not name the field it disliked.
func orderQuery(status []string, tenantID, since, until string) (api.OrderSearchQuery, error) {
	var query api.OrderSearchQuery

	if len(status) > 0 {
		in := make([]api.OrderStatusEnumFilterIn, 0, len(status))
		for _, s := range status {
			v, err := enumValue("status", s, orderStatuses())
			if err != nil {
				return query, err
			}
			in = append(in, api.OrderStatusEnumFilterIn(v))
		}
		query.Status = &api.OrderStatusEnumFilter{In: &in}
	}

	if tenantID != "" {
		query.TenantOrderId = &api.StringFilter{Eq: &tenantID}
	}

	date, err := orderDateFilter(since, until)
	if err != nil {
		return query, err
	}
	query.OrderDate = date

	return query, nil
}

// orderDateFilter builds the orderDate range from --since/--until, or nil if
// neither was given. A bare gte/lte on the same field is how the API expresses a
// half-open or closed range.
func orderDateFilter(since, until string) (*api.DateFilter, error) {
	if since == "" && until == "" {
		return nil, nil
	}

	var filter api.DateFilter
	if since != "" {
		t, err := parseOrderDate("since", since)
		if err != nil {
			return nil, err
		}
		filter.Gte = &t
	}
	if until != "" {
		t, err := parseOrderDate("until", until)
		if err != nil {
			return nil, err
		}
		filter.Lte = &t
	}
	return &filter, nil
}

// parseOrderDate accepts either a plain date or a full RFC 3339 timestamp, so a
// user can write --since 2026-07-01 without spelling out a midnight nobody cares
// about.
func parseOrderDate(flag, v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t, nil
	}
	return time.Time{}, exitcode.UsageError{Err: fmt.Errorf(
		"--%s %q is not a date (2026-07-01) or a timestamp (2026-07-01T00:00:00Z)", flag, v)}
}

// orderSort parses --sort into the API's sort object, which is a struct with
// exactly one field set rather than a string.
func orderSort(v string) ([]api.OrderSort, error) {
	return parseSort(v, orderSortFields, func(s *api.OrderSort, field, dir string) bool {
		switch field {
		case "orderdate":
			s.OrderDate = ptr(api.OrderSortOrderDate(dir))
		case "status":
			s.Status = ptr(api.OrderSortStatus(dir))
		default:
			return false
		}
		return true
	})
}
