package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const orderLong = `Manage the orders of your tenant.

An order is the demand the platform fulfills: what a consumer asked for, where it
goes, and how. It is the object every fulfillment flow starts from — the platform
sources and routes an order into the pickjobs, packjobs and handovers that carry
it out. You do not create those directly; they arise from the order.

An order moves through OPEN, PROMISED, LOCKED, CANCELLED and OBSOLETE. Reading is
cheap; every write is versioned. A mutation reads the order first to learn its
current version and sends that version back — the API rejects a write that carries
a stale one. Pass --if-version to skip that read when you already know the
version: you get a clean 409 instead of a silent overwrite if you were wrong.

<id> is the order's platform id, the one 'fft order list' and 'fft order get'
print. There is no tenantFacilityId-style shorthand for orders.`

func newOrderCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "order",
		Aliases: []string{"orders"},
		Short:   "Manage orders",
		Long:    orderLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newOrderListCmd(deps),
		newOrderSearchCmd(deps),
		newOrderGetCmd(deps),
		newOrderCreateCmd(deps),
		newOrderUpdateCmd(deps),
		newOrderCancelCmd(deps),
		newOrderUnlockCmd(deps),
	)

	return cmd
}

// getOrder reads one order, as the API wrote it. The id is the order's platform
// id — unlike a facility, an order has no URN shorthand to wrap.
func getOrder(ctx context.Context, c *client.Client, id string) ([]byte, error) {
	res, err := c.Do(ctx, fmt.Sprintf("get order %s", id), func(ctx context.Context) (*http.Response, error) {
		return c.API().GetOrder(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// getOrderDoc reads one order and its version together, which is what every
// read-then-write mutation and every versioned action starts with.
func getOrderDoc(ctx context.Context, c *client.Client, id string) (entityDoc, int, error) {
	raw, err := getOrder(ctx, c, id)
	if err != nil {
		return nil, 0, err
	}

	doc, err := decodeDoc(raw, "the order")
	if err != nil {
		return nil, 0, err
	}

	version, err := docVersion(doc, "order")
	if err != nil {
		return nil, 0, err
	}
	return doc, version, nil
}

// The order states. An order starts OPEN and moves through the rest as it is
// promised, locked, cancelled or superseded.
const (
	orderStatusOpen      = "OPEN"
	orderStatusPromised  = "PROMISED"
	orderStatusLocked    = "LOCKED"
	orderStatusCancelled = "CANCELLED"
	orderStatusObsolete  = "OBSOLETE"
)

func orderStatuses() []string {
	return []string{orderStatusOpen, orderStatusPromised, orderStatusLocked, orderStatusCancelled, orderStatusObsolete}
}

// orderView is the table's model of an order: the handful of fields a human
// scanning a list reads. It is written to fit *both* projections the API returns —
// the full Order from get and search, and the reduced StrippedOrder from the GET
// list — so tenantOrderId is a dash on a list the API stripped it from rather than
// a missing column. The columns the two share (id, status, orderDate, line count,
// version) are always present.
type orderView struct {
	ID             string            `json:"id"`
	TenantOrderID  string            `json:"tenantOrderId"`
	Status         string            `json:"status"`
	OrderDate      string            `json:"orderDate"`
	OrderLineItems []json.RawMessage `json:"orderLineItems"`
	Version        int64             `json:"version"`
}

// orderList is the view `fft order list` and `fft order search` render.
func orderList() listView {
	return listView{Noun: "orders", Rows: orderRows}
}

// renderOrder renders one order: the API's own JSON object under -o json, a
// one-row table otherwise. Like renderFacility it is deliberately not a slice of
// one, so `fft order get x -o json | jq .status` sees the object, not a list.
func renderOrder(deps *Deps, raw []byte) error {
	// A 2xx with no body is a legitimate answer to a mutation; there is nothing to
	// render and the notice on stderr has already said what happened.
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	rows, err := orderRows(deps.Printer.Style(), []json.RawMessage{raw})
	if err != nil {
		return err
	}
	return deps.Printer.RenderRaw(rows, raw)
}

var orderHeaders = []string{"ID", "TENANT ID", "STATUS", "ORDER DATE", "LINES", "VERSION"}

// orderRows builds the order table. An order fft cannot parse is a bug worth
// reporting, not a row worth skipping, so a decode failure is a hard error.
func orderRows(style output.Style, items []json.RawMessage) (output.Rows, error) {
	rows := make([][]string, 0, len(items))

	for i, item := range items {
		var v orderView
		if err := json.Unmarshal(item, &v); err != nil {
			return output.Rows{}, fmt.Errorf("decode order %d of %d: %w", i+1, len(items), err)
		}

		rows = append(rows, []string{
			field(style, v.ID),
			field(style, v.TenantOrderID),
			orderStatusCell(style, v.Status),
			field(style, v.OrderDate),
			fmt.Sprintf("%d", len(v.OrderLineItems)),
			fmt.Sprintf("%d", v.Version),
		})
	}
	return output.Rows{Headers: orderHeaders, Rows: rows}, nil
}

// orderStatusCell colours the column a reader scans: an OPEN order is live work,
// a PROMISED or LOCKED one is held, and a CANCELLED or OBSOLETE one is done with.
func orderStatusCell(style output.Style, status string) string {
	switch status {
	case orderStatusOpen:
		return style.Green(status)
	case orderStatusPromised, orderStatusLocked:
		return style.Yellow(status)
	case orderStatusCancelled, orderStatusObsolete:
		return style.Red(status)
	default:
		return field(style, status)
	}
}

// runOrderAction performs a versioned order action against POST
// /api/orders/{id}/actions. Every action body carries a "name" discriminator and
// the order's "version"; the API rejects a stale version with a 409, so this is
// the same read-then-write shape as an update — [client.UpdateVersioned] reads the
// order to learn its version, POSTs the action, and retries once on conflict.
//
// The action carries no entity of its own to mutate, so the mutate step is a
// no-op: put ignores the read doc and assembles {name, version, ...extra} itself.
// With --if-version the read is skipped and the named version is used directly.
func runOrderAction(cmd *cobra.Command, deps *Deps, id, name string, extra entityDoc, version *int, notice string) error {
	c, err := tenantClient(deps)
	if err != nil {
		return err
	}

	ctx, cancel := deps.Context(cmd)
	defer cancel()

	var raw []byte

	get := func(ctx context.Context) (entityDoc, int, error) {
		return getOrderDoc(ctx, c, id)
	}

	put := func(ctx context.Context, _ entityDoc, v int) (entityDoc, error) {
		body := entityDoc{"name": name, "version": v}
		for k, val := range extra {
			body[k] = val
		}

		answer, err := sendEntity(ctx, c, fmt.Sprintf("%s order %s", strings.ToLower(name), id), body,
			func(ctx context.Context, body io.Reader) (*http.Response, error) {
				return c.API().OrderActionWithBody(ctx, id, contentTypeJSON, body)
			})
		if err != nil {
			return nil, err
		}
		raw = answer
		return nil, nil
	}

	noop := func(*entityDoc) error { return nil }

	if _, err := client.UpdateVersioned(ctx, get, put, noop, version); err != nil {
		return err
	}

	deps.Printer.Notef("%s", notice)
	return renderOrder(deps, raw)
}
