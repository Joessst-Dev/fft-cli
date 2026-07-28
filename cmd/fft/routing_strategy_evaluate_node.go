package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

const routingStrategyEvaluateNodeLong = `Dry-run one node of a routing strategy.

Where 'evaluate' walks the whole strategy for an order, this evaluates a single node
and returns the path from it. It reserves nothing and creates nothing — a what-if,
safe against the live strategy.

  fft routing strategy evaluate-node 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 root-node-id

The node id is the one 'fft routing strategy get -o json' prints, and the one
'evaluate' names in its REF column. The table lists the evaluated path; -o json
carries the full result.`

func newRoutingStrategyEvaluateNodeCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "evaluate-node <id> <nodeId>",
		Short: "Dry-run one node of a strategy",
		Long:  routingStrategyEvaluateNodeLong,
		Args:  usageArgs(cobra.ExactArgs(2)),

		Annotations: map[string]string{annotationOperationID: "evaluateRoutingStrategyNode"},

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			strategyID, nodeID := args[0], args[1]
			res, err := c.Do(ctx, fmt.Sprintf("evaluate node %s of routing strategy %s", nodeID, strategyID),
				func(ctx context.Context) (*http.Response, error) {
					return c.API().EvaluateRoutingStrategyNode(ctx, strategyID, nodeID)
				})
			if err != nil {
				return err
			}
			return renderEvaluation(deps, res.Body)
		},
	}
}
