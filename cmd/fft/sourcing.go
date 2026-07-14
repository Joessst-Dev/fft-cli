package main

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const sourcingLong = `Ask the routing engine where an order would be fulfilled from.

A sourcing option is a simulation, not a booking. You describe an order — who it
goes to and what is in it — and the router answers with the ways it could be
fulfilled: which facilities would pick it, which connections it would travel
along, what that would cost, and when it would arrive. Nothing is reserved, no
order is created, and no stock moves. It is safe on a read-only project, and fft
knows that: 'fft sourcing simulate' is a POST that does not write.

  fft sourcing simulate --example > order.json
  $EDITOR order.json
  fft sourcing simulate --file order.json --results 3

Every option names the connections it would use, so this is the other half of
'fft connection': the router tells you which edge it chose, and 'fft connection
get' tells you what that edge is.

Two things to read carefully. An empty answer does not mean "no matches" — it
means the order cannot be routed at all. And the penalty is a penalty: the
option at the top of the table is the one the router likes best.`

func newSourcingCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sourcing",
		Aliases: []string{"sourcingoptions"},
		Short:   "Simulate how an order would be routed",
		Long:    sourcingLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newSourcingSimulateCmd(deps),
		newSourcingGetCmd(deps),
	)

	return cmd
}

// The node types. They are the connection types under another name — a node is a
// facility, a supplier, or the customer — and the router uses the same vocabulary.
const nodeCustomer = "CUSTOMER"

// sourcingResponse is the envelope both commands get back: the run's id, and the
// options it found.
type sourcingResponse struct {
	ID     string `json:"id"`
	Result struct {
		Options []sourcingOption `json:"options"`
	} `json:"result"`
}

// sourcingOption is one way the order could be fulfilled.
//
// Hand-written, like every other view in fft: the table is a summary and -o json is
// the API's own bytes, so nothing in between needs the generated model.
type sourcingOption struct {
	ID string `json:"id"`

	// TotalPenalty is a *penalty*. Lower is better, and the table sorts on it
	// ascending. Presenting it as a score — bigger is better — would invert the
	// router's advice while looking perfectly plausible.
	TotalPenalty float64 `json:"totalPenalty"`

	Nodes []struct {
		ID               string        `json:"id"`
		Type             string        `json:"type"`
		FacilityRef      string        `json:"facilityRef"`
		TenantFacilityID string        `json:"tenantFacilityId"`
		LineItems        []handledItem `json:"lineItems"`
	} `json:"nodes"`

	Transfers []struct {
		SourceNodeRef string `json:"sourceNodeRef"`
		TargetNodeRef string `json:"targetNodeRef"`

		// FacilityConnectionRef is the connection this leg travels along — the join back
		// to `fft connection get`, and the reason this command and that one belong in the
		// same release.
		FacilityConnectionRef string `json:"facilityConnectionRef"`

		Carrier *struct {
			CarrierKey  string `json:"carrierKey"`
			CarrierName string `json:"carrierName"`
		} `json:"carrier"`
	} `json:"transfers"`

	// NonAssignedOrderLineItems is the partial-failure channel. An option can come
	// back looking perfectly healthy and still have quietly dropped items, so this is
	// a column of its own and it is coloured when it is not zero.
	NonAssignedOrderLineItems []handledItem `json:"nonAssignedOrderLineItems"`

	TotalCosts *struct {
		TotalCosts *money `json:"totalCosts"`
	} `json:"totalCosts"`

	EstimatedDeliveryDate string `json:"estimatedDeliveryDate"`
	EstimatedPickupDate   string `json:"estimatedPickupDate"`
	ValidUntil            string `json:"validUntil"`
}

type handledItem struct {
	TenantArticleID string   `json:"tenantArticleId"`
	Quantity        *float64 `json:"quantity"`
}

// money is the API's money everywhere: an integer in the currency's *smallest*
// subunit, plus the number of decimal places that turns it back into one. 25000 with
// decimalPlaces 2 is 250.00, not twenty-five thousand.
type money struct {
	Value         float64  `json:"value"`
	Currency      string   `json:"currency"`
	DecimalPlaces *float64 `json:"decimalPlaces"`
}

// String renders the amount the way a human would write it. The scaling is done in
// the renderer and never on the way in: fft does not parse money into a float and
// hand it back to the API.
func (m *money) String() string {
	if m == nil {
		return ""
	}

	places := 0
	if m.DecimalPlaces != nil {
		places = int(*m.DecimalPlaces)
	}
	if places < 0 || places > 6 {
		// A decimalPlaces fft cannot believe is one it will not scale by. Better to show
		// the raw minor units next to the currency than to move the point at random.
		return fmt.Sprintf("%g %s", m.Value, m.Currency)
	}

	amount := m.Value / math.Pow(10, float64(places))
	return fmt.Sprintf("%.*f %s", places, amount, m.Currency)
}

// totalQuantity totals the items in a list. The API carries a quantity per article,
// and an absent one means one of it.
func totalQuantity(items []handledItem) float64 {
	var total float64
	for _, item := range items {
		if item.Quantity == nil {
			total++
			continue
		}
		total += *item.Quantity
	}
	return total
}

// renderSourcing prints a sourcing run: the API's own JSON under -o json, a table of
// the options otherwise.
//
// The empty case is the one this function exists for. An option list that came back
// empty is a 200 (or a 201) and not an error, and it does not mean "nothing matched
// your query" — it means the router could not fulfil this order at all, from
// anywhere. Rendering that as a bland empty table would be the worst answer
// available: it looks exactly like a successful search with no hits.
func renderSourcing(deps *Deps, raw []byte) error {
	var res sourcingResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("decode the sourcing options: %w", err)
	}

	if len(res.Result.Options) == 0 {
		deps.Printer.Warnf(
			"The router found no way to fulfil this order: no facility, or no combination of them, could source it. This is an answer, not a failure — check that the articles are listed and in stock somewhere, and that a connection reaches the customer.")

		// Under -o json the API's document is still printed, in full. Printer.Empty
		// would write `[]` — which is the right answer for a list command that matched
		// nothing, and the wrong shape entirely for this one: the answer here is an
		// object, and replacing it with an array would throw away the run id that `fft
		// sourcing get` needs and that the notice above just told the user to use.
		if deps.Printer.Format() != output.Table {
			return deps.Printer.RenderRaw(output.Rows{}, raw)
		}
		return deps.Printer.Empty("sourcing options")
	}

	// Ascending: it is a penalty, so the router's favourite is the one at the top.
	options := slices.Clone(res.Result.Options)
	slices.SortStableFunc(options, func(a, b sourcingOption) int {
		switch {
		case a.TotalPenalty < b.TotalPenalty:
			return -1
		case a.TotalPenalty > b.TotalPenalty:
			return 1
		default:
			return 0
		}
	})

	rows, err := sourcingRows(deps.Printer.Style(), options)
	if err != nil {
		return err
	}

	if unsourced := slices.ContainsFunc(options, func(o sourcingOption) bool {
		return len(o.NonAssignedOrderLineItems) > 0
	}); unsourced {
		deps.Printer.Warnf("Some options cannot source every item — see the UNSOURCED column. An option that leaves items behind will not fulfil the whole order.")
	}

	return deps.Printer.RenderRaw(rows, raw)
}

var sourcingHeaders = []string{"#", "PENALTY", "ROUTE", "SOURCED", "UNSOURCED", "COST", "ETA", "VALID UNTIL", "ID"}

func sourcingRows(style output.Style, options []sourcingOption) (output.Rows, error) {
	rows := make([][]string, 0, len(options))

	for i, o := range options {
		var cost string
		if o.TotalCosts != nil {
			cost = o.TotalCosts.TotalCosts.String()
		}

		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1),
			fmt.Sprintf("%g", o.TotalPenalty),
			field(style, route(o)),
			fmt.Sprintf("%g", sourced(o)),
			unsourcedCell(style, o),
			field(style, cost),
			field(style, eta(o)),
			field(style, o.ValidUntil),
			field(style, o.ID),
		})
	}
	return output.Rows{Headers: sourcingHeaders, Rows: rows}, nil
}

// eta is the date this option lands: delivered, or — for a click-and-collect option —
// ready to be picked up. They are different promises, so the one that is not a
// delivery says so.
func eta(o sourcingOption) string {
	if o.EstimatedDeliveryDate != "" {
		return o.EstimatedDeliveryDate
	}
	if o.EstimatedPickupDate != "" {
		return o.EstimatedPickupDate + " (pickup)"
	}
	return ""
}

// sourced is how much of the order this option would actually move.
//
// It counts the goods where they come to rest: the *sink* nodes, the ones nothing
// leaves. Summing the source facilities instead would count an item once per leg it
// travelled, so a two-hop route would claim to have sourced twice the order.
//
// The sink is usually the customer, and counting the CUSTOMER node would very nearly
// work. But it would answer 0 for a click-and-collect option that hops first — goods
// move BER-01 → FRA-02 and the consumer collects at FRA-02, so there are transfers and
// no customer node at all — and an option that says it moves nothing while also
// dropping nothing is a row that describes no possible world. The sink is what both
// cases have in common.
//
// An option with no transfers has no edges, so every node is a sink and there is no leg
// for anything to be double-counted across.
func sourced(o sourcingOption) float64 {
	out := make(map[string]int, len(o.Transfers))
	for _, t := range o.Transfers {
		out[t.SourceNodeRef]++
	}

	var total float64
	for _, n := range o.Nodes {
		if out[n.ID] == 0 {
			total += totalQuantity(n.LineItems)
		}
	}
	return total
}

// unsourcedCell is the column that stops a half-empty option from reading as a
// healthy one. It is coloured for the same reason a facility that is not ONLINE is:
// it is the cell a reader is scanning for.
func unsourcedCell(style output.Style, o sourcingOption) string {
	n := totalQuantity(o.NonAssignedOrderLineItems)
	if n == 0 {
		return style.Faint("0")
	}
	return style.Red(fmt.Sprintf("%g", n))
}

// route renders the path the goods would take, as the chain of nodes the transfers
// connect: "BER-01 → FRA-02 → customer".
//
// It walks the graph from the node nothing transfers *into*, which is where the goods
// start. A branching or cyclic graph — an order split across two facilities — cannot
// be a single chain, so rather than pretending it is one, the nodes are simply listed.
// A route that is a lie reads exactly like a route that is true.
func route(o sourcingOption) string {
	if len(o.Nodes) == 0 {
		return ""
	}

	label := make(map[string]string, len(o.Nodes))
	for _, n := range o.Nodes {
		label[n.ID] = nodeLabel(n.Type, n.FacilityRef, n.TenantFacilityID)
	}

	next := make(map[string]string, len(o.Transfers))
	into := make(map[string]int, len(o.Transfers))
	out := make(map[string]int, len(o.Transfers))

	for _, t := range o.Transfers {
		next[t.SourceNodeRef] = t.TargetNodeRef
		out[t.SourceNodeRef]++
		into[t.TargetNodeRef]++
	}

	// The start is the node with nothing coming into it. Exactly one, or this is not a
	// chain.
	var starts []string
	for id := range label {
		if into[id] == 0 {
			starts = append(starts, id)
		}
	}

	if len(starts) != 1 || len(o.Transfers) == 0 {
		return listNodes(o)
	}

	chain := make([]string, 0, len(o.Nodes))
	seen := make(map[string]bool, len(o.Nodes))

	for id := starts[0]; id != ""; id = next[id] {
		// A cycle, or a fork. Neither is a chain, and following it would either loop
		// forever or quietly drop a branch.
		if seen[id] || out[id] > 1 {
			return listNodes(o)
		}
		seen[id] = true

		name, ok := label[id]
		if !ok {
			return listNodes(o)
		}
		chain = append(chain, name)
	}

	// Every node has to appear, or the chain is a partial account of the option that
	// looks like a complete one.
	if len(chain) != len(o.Nodes) {
		return listNodes(o)
	}
	return strings.Join(chain, " → ")
}

// listNodes is the fallback for an option whose graph is not a single line: the nodes,
// named, in the order the API gave them.
func listNodes(o sourcingOption) string {
	names := make([]string, 0, len(o.Nodes))
	for _, n := range o.Nodes {
		names = append(names, nodeLabel(n.Type, n.FacilityRef, n.TenantFacilityID))
	}
	return strings.Join(names, ", ")
}

// nodeLabel names a node the way its operator would: by the id they gave it, falling
// back to the platform's. The customer is not a facility and has neither.
func nodeLabel(typ, facilityRef, tenantFacilityID string) string {
	switch {
	case typ == nodeCustomer:
		return "customer"
	case tenantFacilityID != "":
		return tenantFacilityID
	case facilityRef != "":
		return facilityRef
	default:
		return strings.ToLower(typ)
	}
}
