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
	// meets the "own source" case; bundlesTo leaves each facility's own
	// connections out of the class's aggregate instead.
	rep Edge
	// byKind aggregates the class's members by the kind of their source. A
	// member's source is the only thing that differs between members, so
	// everything a facility's bundle needs from the class is folded in once
	// here, and a facility only has to leave out the connections it sources
	// itself, which ReachTo skips too.
	byKind map[Kind]*classKind
}

// classKind is the members of one class whose source has one kind.
type classKind struct {
	count   int
	sources Bitset
	// perSource is how many members each source has, so a facility can subtract
	// its own.
	perSource map[int]int
	// minTransit and maxTransit are the shortest and longest fallback transit
	// time, each kept so that one source can be left out.
	minTransit, maxTransit extreme
}

// extreme is the best value of a set and the best among the sources other than
// the one holding it: what a minimum or a maximum becomes when that source is
// left out, which a plain minimum cannot say.
type extreme struct {
	src        int
	val, other float64
	n          int // 0: empty, 1: val only, 2: both
}

func (x *extreme) add(v float64, src int, better func(a, b float64) bool) {
	switch {
	case x.n == 0:
		*x = extreme{src: src, val: v, n: 1}
	case src == x.src:
		if better(v, x.val) {
			x.val = v
		}
	case better(v, x.val):
		x.other, x.val, x.src, x.n = x.val, v, src, 2
	case x.n == 1 || better(v, x.other):
		x.other, x.n = v, 2
	}
}

// without is the best value among the sources other than src.
func (x extreme) without(src int) (float64, bool) {
	switch {
	case x.n == 0:
		return 0, false
	case x.src != src:
		return x.val, true
	case x.n == 2:
		return x.other, true
	}
	return 0, false
}

func lower(a, b float64) bool  { return a < b }
func higher(a, b float64) bool { return a > b }

func (n *Network) edgeClasses(roles Roles) []*edgeClass {
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
			c = &edgeClass{rep: Edge{Target: e.Target, Context: e.Context}, byKind: map[Kind]*classKind{}}
			byKey[key.String()] = c
			out = append(out, c)
		}

		src, ok := n.refs[e.Source]
		if !ok {
			continue
		}
		kind, ok := roles.kind(src)
		if !ok {
			continue
		}
		g := c.byKind[kind]
		if g == nil {
			g = &classKind{sources: NewBitset(len(n.snap.Facilities)), perSource: map[int]int{}}
			c.byKind[kind] = g
		}
		g.count++
		g.sources.Set(src)
		g.perSource[src]++
		if t := e.Transit; t != nil {
			g.minTransit.add(t.Min, src, lower)
			g.maxTransit.add(t.Max, src, higher)
		}
	}
	return out
}

// MaxBundleSources is how many sources a bundle names.
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
	// them all. The connections themselves stay with [Network.ReachTo].
	Sources     []string `json:"sources"`
	SourceCount int      `json:"sourceCount"`
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
//
// It merges each matching class's aggregate instead of walking its members, so
// the work is classes, not connections: a report explains hundreds of
// facilities for several kinds of order.
func (n *Network) bundlesTo(classes []*edgeClass, i int, p Profile) []Bundle {
	f := n.Facility(i)
	type key struct {
		kind           Kind
		result, reason string
	}
	type acc struct {
		bundle  Bundle
		sources Bitset
	}
	byKey := map[key]*acc{}
	var out []*acc
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

		for _, kind := range []Kind{KindHub, KindSupplier, KindStore} {
			g := c.byKind[kind]
			if g == nil {
				continue
			}
			count := g.count - g.perSource[i]
			if count == 0 {
				continue
			}
			bk := key{kind, result, reason}
			a, ok := byKey[bk]
			if !ok {
				a = &acc{bundle: Bundle{Kind: kind, Result: result, Reason: reason},
					sources: NewBitset(len(n.snap.Facilities))}
				byKey[bk] = a
				out = append(out, a)
			}
			a.bundle.Count += count
			a.sources.Or(g.sources)
			if lo, ok := g.minTransit.without(i); ok {
				hi, _ := g.maxTransit.without(i)
				if a.bundle.Transit == nil {
					a.bundle.Transit = &Transit{Min: lo, Max: hi}
				} else {
					a.bundle.Transit.Min = min(a.bundle.Transit.Min, lo)
					a.bundle.Transit.Max = max(a.bundle.Transit.Max, hi)
				}
			}
		}
	}

	rank := map[string]int{ResultApplies: 0, ResultConditional: 1, ResultExcluded: 2}
	kinds := map[Kind]int{KindHub: 0, KindSupplier: 1, KindStore: 2}
	slices.SortFunc(out, func(a, b *acc) int {
		return cmp.Or(cmp.Compare(rank[a.bundle.Result], rank[b.bundle.Result]),
			cmp.Compare(kinds[a.bundle.Kind], kinds[b.bundle.Kind]),
			cmp.Compare(b.bundle.Count, a.bundle.Count), strings.Compare(a.bundle.Reason, b.bundle.Reason))
	})
	bundles := make([]Bundle, len(out))
	for k, a := range out {
		// The facility sources none of its own connections into itself.
		if a.sources.Has(i) {
			a.sources[i/64] &^= 1 << (uint(i) % 64)
		}
		b := a.bundle
		b.SourceCount = a.sources.Count()
		b.Sources = make([]string, 0, min(b.SourceCount, MaxBundleSources))
		a.sources.Each(func(src int) {
			if len(b.Sources) < MaxBundleSources {
				b.Sources = append(b.Sources, n.Facility(src).Label())
			}
		})
		slices.Sort(b.Sources)
		bundles[k] = b
	}
	return bundles
}
