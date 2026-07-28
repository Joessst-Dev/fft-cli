package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const routingDecisionLogsLong = `Show why the router chose what it chose.

A decision log records one routing run: the strategy it evaluated, the sourcing
options it produced, and the refs — order, process, routing plan — that run belonged
to. It is the audit trail behind 'fft sourcing simulate' and every real order.

Filter by the thing you are investigating; the filters are exact-match ids, not
searches:

  fft routing decision-logs --order 0190d6e8-8c3a-7f1b-9d2e-4a6b8c0d1e2f
  fft routing decision-logs --tenant-order-id R456728546
  fft routing decision-logs --routing-plan <ref> -o json | jq '.[].routingStrategyEvaluationResult'

The table is a summary; -o json carries each log in full. This endpoint pages by id,
so --all fetches every match and stops at --max-items.`

func newRoutingDecisionLogsCmd(deps *Deps) *cobra.Command {
	var (
		filter client.RoutingDecisionLogsFilter
		page   pageFlags
	)

	cmd := &cobra.Command{
		Use:     "decision-logs",
		Aliases: []string{"decisionlogs"},
		Short:   "Show routing decision logs",
		Long:    routingDecisionLogsLong,
		Args:    usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "getRoutingDecisionLogs"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			build := func(context.Context, *client.Client) (client.ListOp[json.RawMessage], error) {
				return client.RoutingDecisionLogs(filter), nil
			}
			return runList(cmd, deps, build, page, routingDecisionLogsView())
		},
	}

	f := cmd.Flags()
	f.StringVar(&filter.OrderRef, "order", "", "Only logs for this orderRef")
	f.StringVar(&filter.RoutingPlanRef, "routing-plan", "", "Only logs for this routingPlanRef")
	f.StringVar(&filter.ProcessRef, "process", "", "Only logs for this processRef")
	f.StringVar(&filter.TenantOrderID, "tenant-order-id", "", "Only logs for this tenantOrderId")
	f.StringVar(&filter.SourcingOptionRef, "sourcing-option", "", "Only logs for this sourcingOptionRef")
	f.StringVar(&filter.SourcingOptionsRef, "sourcing-options", "",
		"Only logs for this sourcingOptionsRef (the spec marks this filter deprecated; prefer --sourcing-option)")
	page.register(f, "decision logs", client.DefaultListSize)

	return cmd
}

// routingDecisionLogView is the table's model of a decision log: enough to recognise
// which run a row is, with the detail left to -o json.
type routingDecisionLogView struct {
	ID          string `json:"id"`
	Created     string `json:"created"`
	RelatedRefs struct {
		OrderRef      string `json:"orderRef"`
		ProcessRef    string `json:"processRef"`
		TenantOrderID string `json:"tenantOrderId"`
	} `json:"relatedRefs"`
}

func routingDecisionLogsView() listView {
	return listView{Noun: "decision logs", Rows: routingDecisionLogRows}
}

var routingDecisionLogHeaders = []string{"ID", "TENANT ORDER", "ORDER", "PROCESS", "CREATED"}

func routingDecisionLogRows(style output.Style, items []json.RawMessage) (output.Rows, error) {
	rows := make([][]string, 0, len(items))

	for i, item := range items {
		var v routingDecisionLogView
		if err := json.Unmarshal(item, &v); err != nil {
			return output.Rows{}, fmt.Errorf("decode decision log %d of %d: %w", i+1, len(items), err)
		}

		rows = append(rows, []string{
			field(style, v.ID),
			field(style, v.RelatedRefs.TenantOrderID),
			field(style, v.RelatedRefs.OrderRef),
			field(style, v.RelatedRefs.ProcessRef),
			field(style, v.Created),
		})
	}
	return output.Rows{Headers: routingDecisionLogHeaders, Rows: rows}, nil
}
