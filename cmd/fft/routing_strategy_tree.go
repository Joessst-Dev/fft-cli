package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const routingStrategyTreeLong = `Show a routing strategy as the tree the router walks.

The API stores a strategy as a linked list of conditions; this draws it as the
tree it is. Each condition leads to a node when its rule holds, and the router
takes the first condition that holds, top to bottom. Each node shows the fences
and ratings it adds, which every node below it inherits.

  fft routing strategy tree
  fft routing strategy tree 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 --all
  fft routing strategy tree --order 0190d6e8-8c3a-7f1b-9d2e-4a6b8c0d1e2f

Without an id it shows the strategy in use. Inactive branches are folded into one
line; --all unfolds them.

--order marks the branch the router took for an order, read from the order's most
recent routing decision log: ▶ on what it took, ✗ on a condition it evaluated
and found false. A simulated routing ('fft sourcing simulate') leaves no decision
log, so it cannot be marked.

-o json prints the tree as fft draws it, marks included; 'fft routing strategy
get -o json' prints the API's own document. Every request is a read.`

func newRoutingStrategyTreeCmd(deps *Deps) *cobra.Command {
	var (
		order string
		all   bool
	)

	cmd := &cobra.Command{
		Use:   "tree [<id>]",
		Short: "Show a routing strategy as the tree the router walks",
		Long:  routingStrategyTreeLong,
		Args:  usageArgs(cobra.MaximumNArgs(1)),

		Annotations: map[string]string{
			annotationOperations: "getRoutingStrategies,getRoutingStrategy,getRoutingDecisionLogs",
		},

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := tenantClient(deps)
			if err != nil {
				return err
			}
			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := strategyForTree(ctx, c, args)
			if err != nil {
				return err
			}
			s, err := network.ParseStrategy(raw)
			if err != nil {
				return err
			}

			var marks map[string]string
			if order != "" {
				if marks, err = evaluatedPath(ctx, c, order); err != nil {
					return err
				}
				if !touches(s, marks) {
					deps.Printer.Warnf("the order's decision log names no node or condition of this strategy; "+
						"it was routed with another one (fft routing decision-logs --order %s)", order)
				}
			}

			t := buildTree(s, marks, all)
			if deps.Printer.Format() == output.Table {
				drawTree(deps.Printer.Out(), deps.Printer.Style(), s, t)
				return nil
			}
			return deps.Printer.Render(output.Rows{}, t)
		},
	}

	f := cmd.Flags()
	f.StringVar(&order, "order", "", "Mark the branch the router took for this order (its platform id)")
	f.BoolVar(&all, "all", false, "Unfold inactive branches")
	return cmd
}

// strategyForTree reads the strategy named, or the one in use.
func strategyForTree(ctx context.Context, c *client.Client, args []string) (json.RawMessage, error) {
	if len(args) == 1 {
		return getStrategy(ctx, c, args[0])
	}
	for raw, err := range client.ListAll(ctx, c, client.RoutingStrategies(), client.PageSize) {
		if err != nil {
			return nil, err
		}
		var v struct {
			InUse bool `json:"inUse"`
		}
		if json.Unmarshal(raw, &v) == nil && v.InUse {
			return raw, nil
		}
	}
	return nil, exitcode.UsageError{Err: errors.New(
		"no routing strategy is in use; name one: fft routing strategy tree <id> (fft routing strategy list shows them)")}
}

// evaluatedPath reads the order's most recent decision log and returns what
// it evaluated, by ref: CONDITION_TRUE, CONDITION_FALSE or NODE_ACTIVE.
func evaluatedPath(ctx context.Context, c *client.Client, order string) (map[string]string, error) {
	page, err := client.List(ctx, c, client.RoutingDecisionLogs(client.RoutingDecisionLogsFilter{OrderRef: order}), "", client.MaxSize)
	if err != nil {
		return nil, err
	}
	type log struct {
		Created    string `json:"created"`
		Evaluation struct {
			Path []struct {
				Ref    string `json:"ref"`
				Result string `json:"evaluationResult"`
			} `json:"evaluatedPath"`
		} `json:"routingStrategyEvaluationResult"`
	}
	var latest *log
	for _, raw := range page.Items {
		var l log
		if json.Unmarshal(raw, &l) != nil || len(l.Evaluation.Path) == 0 {
			continue
		}
		if latest == nil || l.Created > latest.Created {
			latest = &l
		}
	}
	if latest == nil {
		return nil, noDecisionLogError{order: order}
	}
	marks := make(map[string]string, len(latest.Evaluation.Path))
	for _, e := range latest.Evaluation.Path {
		marks[e.Ref] = e.Result
	}
	return marks, nil
}

func touches(s *network.Strategy, marks map[string]string) bool {
	found := false
	var visit func(n *network.StrategyNode)
	visit = func(n *network.StrategyNode) {
		if _, ok := marks[n.ID]; ok {
			found = true
		}
		for _, c := range n.Conditions {
			if _, ok := marks[c.ID]; ok {
				found = true
			}
			if c.Next != nil {
				visit(c.Next)
			}
		}
	}
	if s.Root != nil {
		visit(s.Root)
	}
	return found
}

// treeNode is the tree as fft draws it, which -o json prints.
type treeNode struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Active          bool            `json:"active"`
	Fences          []string        `json:"fences,omitempty"`
	Ratings         []string        `json:"ratings,omitempty"`
	PriorityRatings []string        `json:"priorityRatings,omitempty"`
	Fallback        []string        `json:"fallbackFacilities,omitempty"`
	Evaluated       string          `json:"evaluated,omitempty"`
	Conditions      []treeCondition `json:"conditions,omitempty"`
}

type treeCondition struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Active    bool      `json:"active"`
	Rule      string    `json:"rule,omitempty"`
	Evaluated string    `json:"evaluated,omitempty"`
	Folded    int       `json:"folded,omitempty"`
	Next      *treeNode `json:"node,omitempty"`
}

func buildTree(s *network.Strategy, marks map[string]string, all bool) *treeNode {
	if s.Root == nil {
		return nil
	}
	active := func(items []network.ConfigItem) []string {
		var out []string
		for _, it := range items {
			if it.Active {
				out = append(out, it.Label())
			}
		}
		return out
	}
	var build func(n *network.StrategyNode) *treeNode
	build = func(n *network.StrategyNode) *treeNode {
		t := &treeNode{ID: n.ID, Name: n.Name, Active: n.Active, Evaluated: marks[n.ID],
			Fences: active(n.Config.Fences), Ratings: active(n.Config.Ratings),
			PriorityRatings: active(n.Config.PriorityRatings)}
		if f := n.Config.Fallback; f != nil && f.Active {
			t.Fallback = f.Facilities
		}
		for _, c := range n.Conditions {
			tc := treeCondition{ID: c.ID, Name: c.Name, Active: c.Active, Rule: ruleText(c.Rule), Evaluated: marks[c.ID]}
			dormant := !c.Active || (c.Next != nil && !c.Next.Active)
			switch {
			case c.Next == nil:
			case dormant && !all && marks[c.ID] == "":
				tc.Folded = c.Next.Count()
			default:
				tc.Next = build(c.Next)
			}
			t.Conditions = append(t.Conditions, tc)
		}
		return t
	}
	return build(s.Root)
}

// ruleText renders a condition's rule as one line: each predicate as path,
// operator and value, joined by the rule's connector.
func ruleText(raw json.RawMessage) string {
	var r struct {
		Connector  string `json:"predicateConnector"`
		Predicates []struct {
			Path     string `json:"propertyPath"`
			Operator string `json:"entityOperator"`
			Expected any    `json:"expectedValue"`
		} `json:"predicates"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &r) != nil || len(r.Predicates) == 0 {
		return ""
	}
	parts := make([]string, len(r.Predicates))
	for i, p := range r.Predicates {
		parts[i] = strings.TrimSpace(fmt.Sprintf("%s %s %v", p.Path, p.Operator, cmp.Or(fmt.Sprint(p.Expected), "")))
		if p.Expected == nil {
			parts[i] = strings.TrimSpace(p.Path + " " + p.Operator)
		}
	}
	return strings.Join(parts, " "+cmp.Or(r.Connector, "AND")+" ")
}

func drawTree(w io.Writer, style output.Style, s *network.Strategy, t *treeNode) {
	state := "not in use"
	if s.InUse {
		state = "in use"
	}
	fmt.Fprintf(w, "%s (%s, revision %d)\n", s.Name, state, s.Revision)
	if t == nil {
		fmt.Fprintln(w, "  (no root node)")
		return
	}
	fmt.Fprintln(w, nodeLine(style, t))
	drawConditions(w, style, t.Conditions, "")
}

func drawConditions(w io.Writer, style output.Style, conds []treeCondition, indent string) {
	for i, c := range conds {
		branch, next := "├─ ", "│  "
		if i == len(conds)-1 {
			branch, next = "└─ ", "   "
		}
		line := indent + branch + mark(c.Evaluated) + "when " + c.Name
		if c.Rule != "" {
			line += style.Faint("  (" + shorten(c.Rule, 90) + ")")
		}
		switch {
		case c.Folded > 0 && !c.Active:
			line += style.Faint("  inactive; " + countOf(c.Folded, "node") + " folded")
		case c.Folded > 0:
			line += style.Faint("  its node is inactive; " + countOf(c.Folded, "node") + " folded")
		case !c.Active:
			line += style.Faint("  inactive")
		}
		fmt.Fprintln(w, line)
		if c.Next != nil {
			fmt.Fprintln(w, indent+next+"└─ "+nodeLine(style, c.Next))
			drawConditions(w, style, c.Next.Conditions, indent+next+"   ")
		}
	}
}

func nodeLine(style output.Style, t *treeNode) string {
	line := mark(t.Evaluated) + t.Name
	var parts []string
	if len(t.Fences) > 0 {
		parts = append(parts, countOf(len(t.Fences), "fence"))
	}
	if len(t.Ratings) > 0 {
		parts = append(parts, countOf(len(t.Ratings), "rating"))
	}
	if len(t.PriorityRatings) > 0 {
		parts = append(parts, countOf(len(t.PriorityRatings), "priority rating"))
	}
	if len(t.Fallback) > 0 {
		parts = append(parts, "fallback "+strings.Join(t.Fallback, ", "))
	}
	if len(parts) > 0 {
		line += style.Faint("  [" + strings.Join(parts, ", ") + "]")
	}
	if !t.Active {
		line += style.Faint("  inactive")
	}
	return line
}

func mark(evaluated string) string {
	switch evaluated {
	case "CONDITION_TRUE", "NODE_ACTIVE":
		return "▶ "
	case "CONDITION_FALSE":
		return "✗ "
	}
	return ""
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// noDecisionLogError is an order with nothing to mark: no decision log, or none
// that evaluated a strategy. It exits 6, as a missing resource does.
type noDecisionLogError struct{ order string }

func (e noDecisionLogError) Error() string {
	return fmt.Sprintf("order %s has no routing decision log with an evaluated path; it may not have been routed yet", e.order)
}

// ExitCode is 6: what was asked for does not exist.
func (noDecisionLogError) ExitCode() int { return exitcode.NotFound }
