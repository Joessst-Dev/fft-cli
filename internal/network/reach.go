package network

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
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

// edgeClass is connections that differ only in their source: the same target,
// exclusions and context. A network repeats one wildcard connection on every
// store, so a few thousand classes stand for a hundred thousand connections,
// and explaining a facility class by class is what lets a report explain
// hundreds of them.
type edgeClass struct {
	// rep is the class's connection with no source, so explaining it never
	// meets the "own source" case; that is decided member by member.
	rep     Edge
	members []int
}

func (n *Network) edgeClasses() []*edgeClass {
	byKey := map[string]*edgeClass{}
	var out []*edgeClass
	for k := range n.snap.Edges {
		e := &n.snap.Edges[k]
		var key strings.Builder
		key.WriteString(e.Target.Type + "\x00" + e.Target.FacilityRef + "\x00")
		key.WriteString(strings.Join(e.Target.ExcludedFacilities, ",") + "\x00")
		key.WriteString(strings.Join(e.Target.ExcludedGroups, ",") + "\x00")
		for _, c := range e.Context {
			key.WriteString(c.Type + " " + c.Operator + " " + strconv.Itoa(c.Values) + "\x00")
		}
		c, ok := byKey[key.String()]
		if !ok {
			c = &edgeClass{rep: Edge{Target: e.Target, Context: e.Context}}
			byKey[key.String()] = c
			out = append(out, c)
		}
		c.members = append(c.members, k)
	}
	return out
}

// MaxBundleSources is how many sources and connections a bundle names.
const MaxBundleSources = 20

// Bundle is the connections from one kind of source that do the same thing for
// a facility, for the same reason: what [Network.ReachTo] lists row by row,
// counted. On a real network one context entry excludes thousands of
// connections, and the bundle is that sentence once.
type Bundle struct {
	Kind   Kind   `json:"kind"`
	Result string `json:"result"`
	// Reason is ReachTo's reason without the context entry's position, which
	// differs from connection to connection while the reason does not.
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	// Sources names up to MaxBundleSources of the sources, SourceCount counts
	// them all; Connections names up to MaxBundleSources of the connections.
	Sources     []string `json:"sources"`
	SourceCount int      `json:"sourceCount"`
	Connections []string `json:"connections"`
	// Transit is the shortest and longest fallback transit time among the
	// connections that have one.
	Transit *Transit `json:"transit,omitempty"`
}

var entryPosition = regexp.MustCompile(`^context\[\d+\] `)

// BundleReason is a reach reason as a bundle states it.
func BundleReason(why string) string { return entryPosition.ReplaceAllString(why, "") }

// bundlesTo is ReachTo for the facility at i and p, counted into bundles, in
// ReachTo's order: what applies first, hubs before suppliers before stores,
// then the largest bundle.
func (n *Network) bundlesTo(classes []*edgeClass, i int, p Profile, roles Roles) []Bundle {
	f := n.Facility(i)
	type key struct {
		kind           Kind
		result, reason string
	}
	byKey := map[key]*Bundle{}
	sources := map[key]map[string]bool{}
	var out []*Bundle
	for _, c := range classes {
		t := c.rep.Target
		if t.Type == TypeCustomer {
			continue
		}
		if t.FacilityRef != "" {
			if j, ok := n.refs[t.FacilityRef]; !ok || j != i {
				continue
			}
		} else if t.Type != f.Type {
			continue
		}
		why := n.Explain(&c.rep, i, p)
		result := ResultExcluded
		switch {
		case why.Conditional:
			result = ResultConditional
		case why.Applies:
			result = ResultApplies
		}
		reason := BundleReason(why.Why)

		for _, k := range c.members {
			e := &n.snap.Edges[k]
			src, ok := n.refs[e.Source]
			if !ok || src == i {
				continue
			}
			kind, ok := roles.kind(src)
			if !ok {
				continue
			}
			bk := key{kind, result, reason}
			b, ok := byKey[bk]
			if !ok {
				b = &Bundle{Kind: kind, Result: result, Reason: reason, Sources: []string{}, Connections: []string{}}
				byKey[bk] = b
				sources[bk] = map[string]bool{}
				out = append(out, b)
			}
			b.Count++
			if len(b.Connections) < MaxBundleSources {
				b.Connections = append(b.Connections, e.ID)
			}
			if label := n.Facility(src).Label(); !sources[bk][label] {
				sources[bk][label] = true
				b.SourceCount++
				if len(b.Sources) < MaxBundleSources {
					b.Sources = append(b.Sources, label)
				}
			}
			if t := e.Transit; t != nil {
				if b.Transit == nil {
					b.Transit = &Transit{Min: t.Min, Max: t.Max}
				} else {
					b.Transit.Min = min(b.Transit.Min, t.Min)
					b.Transit.Max = max(b.Transit.Max, t.Max)
				}
			}
		}
	}

	rank := map[string]int{ResultApplies: 0, ResultConditional: 1, ResultExcluded: 2}
	kinds := map[Kind]int{KindHub: 0, KindSupplier: 1, KindStore: 2}
	slices.SortFunc(out, func(a, b *Bundle) int {
		return cmp.Or(cmp.Compare(rank[a.Result], rank[b.Result]), cmp.Compare(kinds[a.Kind], kinds[b.Kind]),
			cmp.Compare(b.Count, a.Count), strings.Compare(a.Reason, b.Reason))
	})
	bundles := make([]Bundle, len(out))
	for k, b := range out {
		slices.Sort(b.Sources)
		slices.Sort(b.Connections)
		bundles[k] = *b
	}
	return bundles
}
