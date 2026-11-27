package network

import (
	"cmp"
	"slices"
	"strings"
)

// ReachRow is one connection that could lead to a facility, and what it does
// for one order profile.
type ReachRow struct {
	Connection string   `json:"connection"`
	Source     string   `json:"source"`
	Kind       Kind     `json:"kind"`
	Result     string   `json:"result"`
	Entry      int      `json:"entry"`
	Reason     string   `json:"reason"`
	Transit    *Transit `json:"transit,omitempty"`
}

// The results a reach row can have.
const (
	ResultApplies     = "applies"
	ResultConditional = "conditional"
	ResultExcluded    = "excluded"
)

// ReachTo lists every connection from an online source whose target could be
// the facility at i — it names it, or it is a wildcard of its type — and
// whether it leads there for p, with the reason. It is the question behind
// every no-source and peer-coverage finding: which connection should reach this
// store, and which context entry stops it?
func (n *Network) ReachTo(i int, p Profile, roles Roles) []ReachRow {
	f := n.Facility(i)
	var out []ReachRow
	for k := range n.snap.Edges {
		e := &n.snap.Edges[k]
		src, ok := n.refs[e.Source]
		if !ok || src == i || e.Target.Type == TypeCustomer {
			continue
		}
		kind, ok := roles.kind(src)
		if !ok {
			continue
		}
		if ref := e.Target.FacilityRef; ref != "" {
			if j, ok := n.refs[ref]; !ok || j != i {
				continue
			}
		} else if e.Target.Type != f.Type {
			continue
		}

		why := n.Explain(e, i, p)
		result := ResultExcluded
		switch {
		case why.Conditional:
			result = ResultConditional
		case why.Applies:
			result = ResultApplies
		}
		out = append(out, ReachRow{Connection: e.ID, Source: n.Facility(src).Label(), Kind: kind,
			Result: result, Entry: why.Entry, Reason: why.Why, Transit: e.Transit})
	}

	rank := map[string]int{ResultApplies: 0, ResultConditional: 1, ResultExcluded: 2}
	kinds := map[Kind]int{KindHub: 0, KindSupplier: 1, KindStore: 2}
	slices.SortFunc(out, func(a, b ReachRow) int {
		return cmp.Or(cmp.Compare(rank[a.Result], rank[b.Result]), cmp.Compare(kinds[a.Kind], kinds[b.Kind]),
			strings.Compare(a.Source, b.Source), strings.Compare(a.Connection, b.Connection))
	})
	return out
}

// Locate finds a facility by tenant id, platform id or URN.
func (n *Network) Locate(ref string) (int, bool) { return n.lookupAny(ref) }
