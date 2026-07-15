package main

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const orderListLong = `List the orders of your tenant.

This is GET /api/orders: it pages by startAfterId and returns a reduced projection
of each order — id, status, orderDate, line count and version. It filters only on
tenantOrderId and consumerId, both exact matches. For status, a date range or a
sort, use 'fft order search', which is the richer POST /api/orders/search.

By default you get the first page. --all pages to the end, and says so on stderr
if it stops early rather than pretending it reached it.

  fft order list --consumer-id C-4711
  fft order list --all -o json | jq -r '.[].id'
  fft order list --tenant-order-id ORD-2026-0001 --total

stdout carries the orders and nothing else. The total, the truncation notice and
every other remark go to stderr, so a pipe into jq is never contaminated.`

func newOrderListCmd(deps *Deps) *cobra.Command {
	var (
		tenantOrderID string
		consumerID    string
		page          pageFlags
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List orders",
		Long:  orderListLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "getAllOrders"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			build := func(context.Context, *client.Client) (client.ListOp[json.RawMessage], error) {
				return client.Orders(tenantOrderID, consumerID), nil
			}
			return runList(cmd, deps, build, page, orderList())
		},
	}

	f := cmd.Flags()
	f.StringVar(&tenantOrderID, "tenant-order-id", "", "Only the order with this tenantOrderId")
	f.StringVar(&consumerID, "consumer-id", "", "Only orders for this consumerId")
	page.register(f, "orders", client.DefaultListSize)

	return cmd
}
