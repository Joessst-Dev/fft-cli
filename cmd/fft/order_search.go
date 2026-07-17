package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const orderSearchLong = `Search the orders of your tenant.

This is POST /api/orders/search: unlike 'fft order list' it returns whole orders
and filters on status, a date range and a sort. It is BETA, and it is guarded by
the ADMIN_MODULES_READ permission — not ORDER_READ — so a token that lists orders
fine may be refused here.

A narrow --since/--until is strongly recommended: the API pages far faster when
the orderDate range is bounded.

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
		Short: "Search orders (BETA)",
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

			return runSearch(cmd, deps, client.OrderSearch[json.RawMessage](), staticQuery(payload), page, orderList())
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
