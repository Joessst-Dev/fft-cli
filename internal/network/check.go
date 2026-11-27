package network

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// The rule ids. They are part of the output contract: a CI job suppresses a
// finding by its rule id, and an agent branches on it.
const (
	RuleNoSource      = "no-source"
	RulePeerCoverage  = "peer-coverage"
	RuleDanglingRef   = "dangling-ref"
	RuleOfflineRef    = "offline-ref"
	RuleDuplicateEdge = "duplicate-edge"
	// RuleExpect reports a declared expectation a facility does not meet.
	RuleExpect = "expect"
	// RuleConfigDrift reports a config file the network has moved away from.
	RuleConfigDrift = "config-drift"
)

// RuleIDs are the rules a check runs, in the order it reports them.
func RuleIDs() []string {
	return []string{RuleNoSource, RulePeerCoverage, RuleExpect, RuleDanglingRef, RuleOfflineRef,
		RuleDuplicateEdge, RuleConfigDrift}
}

// Finding is one problem a check found.
type Finding struct {
	Rule string `json:"rule"`
	// Subject is what the finding is about: a facility's tenant id, a reference
	// that resolves to nothing, or a connection's source.
	Subject string `json:"subject"`
	// Facilities are the facilities involved, by tenant id where they have one.
	Facilities []string `json:"facilities,omitempty"`
	// Profiles are the order profiles the finding holds for, when it depends on
	// the order at all.
	Profiles []string `json:"profiles,omitempty"`
	Detail   string   `json:"detail"`
	// Evidence is what the finding was concluded from: counts, shares, the ids
	// of the connections involved.
	Evidence map[string]any `json:"evidence,omitempty"`
	// Next is what a person, or an agent, should look at first.
	Next string `json:"next"`
}

// Coverage is how many of a peer group's stores some source of one kind
// reaches, for the profile where the fewest are.
type Coverage struct {
	Kind    Kind   `json:"kind"`
	Group   string `json:"group"`
	Have    int    `json:"have"`
	Size    int    `json:"size"`
	Profile string `json:"profile"`
}

// Report is a check's result: what it decided about the network, and what it
// found.
type Report struct {
	Roles    Roles
	Peers    []PeerGroup
	Profiles []ProfileCount
	// Coverage is reported because the outlier rule cannot see a gap most of a
	// peer group shares: 20% coverage is no outlier, and is a finding for a
	// person to judge.
	Coverage []Coverage
	Findings []Finding
	// Suppressed are the findings a suppression in force accepted. They are
	// counted on stderr, so a suppression can never hide that it is hiding.
	Suppressed []Finding
}

// Check runs the rules cfg selects over the network.
func (n *Network) Check(cfg Config) Report {
	r := Report{Roles: n.InferRoles(cfg)}
	r.Peers = n.InferPeers(cfg, r.Roles)
	r.Profiles = n.InferProfiles(cfg)

	run := func(rule string) bool { return len(cfg.Rules) == 0 || slices.Contains(cfg.Rules, rule) }

	if run(RuleNoSource) || run(RulePeerCoverage) {
		noSource, peer, coverage := n.coverage(cfg, r)
		r.Coverage = coverage
		if run(RuleNoSource) {
			r.Findings = append(r.Findings, noSource...)
		}
		if run(RulePeerCoverage) {
			r.Findings = append(r.Findings, peer...)
		}
	}
	if run(RuleDanglingRef) {
		r.Findings = append(r.Findings, n.danglingRefs()...)
	}
	if run(RuleOfflineRef) {
		r.Findings = append(r.Findings, n.offlineRefs()...)
	}
	if run(RuleDuplicateEdge) {
		r.Findings = append(r.Findings, n.duplicateEdges()...)
	}
	if run(RuleExpect) {
		r.Findings = append(r.Findings, n.expectations(cfg, r)...)
	}
	if run(RuleConfigDrift) && cfg.FromFile {
		r.Findings = append(r.Findings, n.drift(cfg, r)...)
	}

	kept := r.Findings[:0]
	for _, f := range r.Findings {
		if n.suppressed(cfg, f) {
			r.Suppressed = append(r.Suppressed, f)
		} else {
			kept = append(kept, f)
		}
	}
	r.Findings = kept

	// By rule, then by subject: the same snapshot gives the same report, line for
	// line, so two runs can be diffed.
	order := RuleIDs()
	slices.SortStableFunc(r.Findings, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(slices.Index(order, a.Rule), slices.Index(order, b.Rule)),
			strings.Compare(a.Subject, b.Subject))
	})
	return r
}

// coverGroup is every connection from one kind of source that applies to the
// same orders: the targets they reach together, and the order-dependent entries
// they share. A network of a hundred thousand connections has a few hundred of
// these, which is what makes checking every profile affordable.
type coverGroup struct {
	kind Kind
	rep  Edge
	mask Bitset
}

func (n *Network) coverGroups(roles Roles) []*coverGroup {
	byKey := map[string]*coverGroup{}
	var groups []*coverGroup
	for i := range n.snap.Edges {
		e := &n.snap.Edges[i]
		src, ok := n.refs[e.Source]
		if !ok || e.Target.Type == TypeCustomer {
			continue
		}
		kind, ok := roles.kind(src)
		if !ok {
			continue
		}

		var key strings.Builder
		key.WriteString(string(kind))
		var dependent []ContextEntry
		for _, c := range e.Context {
			if knownOperator(c.Operator) && (c.Type == ContextFacility || c.Type == ContextFacilityGroup) {
				continue
			}
			dependent = append(dependent, c)
			fmt.Fprintf(&key, "|%s %s %d", c.Type, c.Operator, c.Values)
		}

		g, ok := byKey[key.String()]
		if !ok {
			g = &coverGroup{kind: kind, rep: Edge{Context: dependent}, mask: n.empty()}
			byKey[key.String()] = g
			groups = append(groups, g)
		}
		g.mask.Or(n.StaticTargets(e))
	}
	return groups
}

// gap is one store's coverage problem, collected across profiles.
type gap struct {
	profiles    []string
	conditional bool
	group       string
	share       string
}

// coverage runs the two reachability rules: no-source (no online source
// reaches a store at all) and peer-coverage (a store lacks a kind of source
// nearly all of its peers have).
func (n *Network) coverage(cfg Config, r Report) (noSource, peer []Finding, coverage []Coverage) {
	groups := n.coverGroups(r.Roles)
	kinds := []Kind{KindHub, KindSupplier}

	none := map[int]*gap{}
	lacking := map[Kind]map[int]*gap{KindHub: {}, KindSupplier: {}}
	worst := map[string]*Coverage{}

	for _, pc := range r.Profiles {
		p := pc.Profile
		reach := map[Kind]Bitset{KindHub: n.empty(), KindStore: n.empty(), KindSupplier: n.empty()}
		conditional := n.empty()
		// Per kind as well, for peer-coverage: a hub that reaches a store only for
		// some items has not left it out, and the finding must not say it did.
		conditionalBy := map[Kind]Bitset{KindHub: n.empty(), KindStore: n.empty(), KindSupplier: n.empty()}
		for _, g := range groups {
			switch v, _ := n.Applies(&g.rep, p); v {
			case Applies:
				reach[g.kind].Or(g.mask)
			case Conditional:
				conditional.Or(g.mask)
				conditionalBy[g.kind].Or(g.mask)
			}
		}

		reached := reach[KindHub].Clone()
		reached.Or(reach[KindStore])
		reached.Or(reach[KindSupplier])
		r.Roles.Stores.Each(func(i int) {
			if reached.Has(i) {
				return
			}
			g, ok := none[i]
			if !ok {
				g = &gap{}
				none[i] = g
			}
			g.profiles = append(g.profiles, p.String())
			g.conditional = g.conditional || conditional.Has(i)
		})

		for _, kind := range kinds {
			sources := r.Roles.Hubs
			if kind == KindSupplier {
				sources = r.Roles.Suppliers
			}
			if sources.Empty() {
				continue
			}
			for _, pg := range r.Peers {
				covered := pg.members.Clone()
				covered.And(reach[kind])
				have := covered.Count()

				key := string(kind) + "\x00" + pg.Key
				if w, ok := worst[key]; !ok || have < w.Have {
					worst[key] = &Coverage{Kind: kind, Group: pg.Key, Have: have, Size: pg.Size, Profile: p.String()}
				}

				if pg.Size < cfg.PeerMinSize || float64(have)/float64(pg.Size) < cfg.PeerShare || have == pg.Size {
					continue
				}
				missing := pg.members.Clone()
				missing.AndNot(reach[kind])
				missing.Each(func(i int) {
					g, ok := lacking[kind][i]
					if !ok {
						g = &gap{group: pg.Key, share: share(have, pg.Size)}
						lacking[kind][i] = g
					}
					g.profiles = append(g.profiles, p.String())
					g.conditional = g.conditional || conditionalBy[kind].Has(i)
				})
			}
		}
	}

	all := len(r.Profiles)
	for _, i := range sortedKeys(none) {
		g := none[i]
		f := n.Facility(i)
		detail := "no online source reaches it"
		if g.conditional {
			detail += ", except connections that depend on an item's category or the recipient"
		}
		noSource = append(noSource, Finding{
			Rule:       RuleNoSource,
			Subject:    f.Label(),
			Facilities: []string{f.Label()},
			Profiles:   g.profiles,
			Detail:     fmt.Sprintf("%s, for %d of %d order profiles", detail, len(g.profiles), all),
			Evidence:   map[string]any{"profiles": len(g.profiles), "of": all, "conditional": g.conditional},
			Next: "Read the connections that should reach it — the hubs' and suppliers' — and find the " +
				"context entry or exclusion that leaves it out. Fixing it is the network owner's decision.",
		})
	}
	for _, kind := range kinds {
		for _, i := range sortedKeys(lacking[kind]) {
			g := lacking[kind][i]
			f := n.Facility(i)
			detail := fmt.Sprintf("no %s reaches it, though one reaches %s of its peers in %s",
				kind, g.share, g.group)
			if g.conditional {
				detail += ", except connections that depend on an item's category or the recipient"
			}
			peer = append(peer, Finding{
				Rule:       RulePeerCoverage,
				Subject:    f.Label(),
				Facilities: []string{f.Label()},
				Profiles:   g.profiles,
				Detail:     detail,
				Evidence: map[string]any{"kind": string(kind), "peerGroup": g.group, "peerShare": g.share,
					"profiles": len(g.profiles), "of": all, "conditional": g.conditional},
				Next: fmt.Sprintf("Compare it with a peer in %s that a %s does reach: the difference is in "+
					"the %s's connections — a context entry, an exclusion, or a group it is missing from.",
					g.group, kind, kind),
			})
		}
	}

	for _, w := range worst {
		coverage = append(coverage, *w)
	}
	slices.SortFunc(coverage, func(a, b Coverage) int {
		return cmp.Or(strings.Compare(string(a.Kind), string(b.Kind)), cmp.Compare(b.Size, a.Size),
			strings.Compare(a.Group, b.Group))
	})
	return noSource, peer, coverage
}

// danglingRefs reports every reference a connection makes to something the
// snapshot does not have: a facility, a group, or a tag value no tag allows.
func (n *Network) danglingRefs() []Finding {
	type use struct {
		what        string
		connections []string
		sources     map[string]bool
	}
	uses := map[string]*use{}
	note := func(what, ref string, e *Edge) {
		key := what + "\x00" + ref
		u, ok := uses[key]
		if !ok {
			u = &use{what: what, sources: map[string]bool{}}
			uses[key] = u
		}
		u.connections = append(u.connections, e.ID)
		u.sources[n.label(e.Source)] = true
	}

	allowed := map[string]bool{}
	for _, t := range n.snap.Tags {
		for _, v := range t.AllowedValues {
			allowed[v] = true
		}
	}

	for i := range n.snap.Edges {
		e := &n.snap.Edges[i]
		if ref := e.Target.FacilityRef; ref != "" && !n.has(ref) {
			note("target facility", ref, e)
		}
		for _, ref := range e.Target.ExcludedFacilities {
			if !n.has(ref) {
				note("excluded facility", ref, e)
			}
		}
		for _, g := range e.Target.ExcludedGroups {
			if !n.HasGroup(g) {
				note("excluded facility group", g, e)
			}
		}
		for _, c := range e.Context {
			for _, v := range n.Values(c) {
				switch {
				case c.Type == ContextFacility && !n.has(v):
					note("FACILITY context value", v, e)
				case c.Type == ContextFacilityGroup && !n.HasGroup(v):
					note("FACILITY_GROUP context value", v, e)
				case c.Type == ContextTag && len(allowed) > 0 && !allowed[v]:
					note("TAG_REFERENCE context value", v, e)
				}
			}
		}
	}

	var out []Finding
	for _, key := range sortedStrings(uses) {
		u := uses[key]
		ref := key[strings.IndexByte(key, 0)+1:]
		out = append(out, Finding{
			Rule:       RuleDanglingRef,
			Subject:    ref,
			Facilities: sortedStrings(u.sources),
			Detail: fmt.Sprintf("the %s %s names nothing in the network, in %s",
				u.what, ref, plural(len(u.connections), "connection")),
			Evidence: map[string]any{"kind": u.what, "connections": capped(u.connections)},
			Next: "It was deleted or mistyped. A positive entry naming only it matches nothing; " +
				"an exclusion or NOT naming it does nothing. Correct it or remove it.",
		})
	}
	return out
}

// offlineRefs reports every offline facility a connection still leads to, once
// per facility: an explicit target, or a positive FACILITY context value.
func (n *Network) offlineRefs() []Finding {
	refs := map[int][]string{}
	for i := range n.snap.Edges {
		e := &n.snap.Edges[i]
		named := map[int]bool{}
		if j, ok := n.refs[e.Target.FacilityRef]; ok && e.Target.FacilityRef != "" {
			named[j] = true
		}
		for _, c := range e.Context {
			if c.Type != ContextFacility || c.Operator != "" {
				continue
			}
			for _, v := range n.Values(c) {
				if j, ok := n.refs[v]; ok {
					named[j] = true
				}
			}
		}
		for j := range named {
			if !n.Facility(j).Online() {
				refs[j] = append(refs[j], e.ID)
			}
		}
	}

	var out []Finding
	for _, j := range sortedKeys(refs) {
		f := n.Facility(j)
		out = append(out, Finding{
			Rule:       RuleOfflineRef,
			Subject:    f.Label(),
			Facilities: []string{f.Label()},
			Detail: fmt.Sprintf("it is %s, and %s still lead to it",
				strings.ToLower(f.Status), plural(len(refs[j]), "connection")),
			Evidence: map[string]any{"status": f.Status, "connections": capped(refs[j])},
			Next: "If it is gone for good, its connections and the context entries naming it are dead " +
				"weight; if it is coming back, nothing needs doing.",
		})
	}
	return out
}

// duplicateEdges reports connections of one source that are the same
// connection: the same target, the same exclusions and the same context, in
// whatever order.
func (n *Network) duplicateEdges() []Finding {
	byKey := map[string][]string{}
	edges := map[string][]*Edge{}
	for i := range n.snap.Edges {
		e := &n.snap.Edges[i]
		key := n.edgeKey(e)
		byKey[key] = append(byKey[key], e.ID)
		edges[key] = append(edges[key], e)
	}
	var out []Finding
	for _, key := range sortedStrings(byKey) {
		ids := byKey[key]
		if len(ids) < 2 {
			continue
		}
		source := n.label(key[:strings.IndexByte(key, 0)])
		f := Finding{
			Rule:       RuleDuplicateEdge,
			Subject:    source,
			Facilities: []string{source},
			Detail:     fmt.Sprintf("%d connections of %s are the same connection: %s", len(ids), source, strings.Join(capped(ids), ", ")),
			Evidence:   map[string]any{"connections": capped(ids)},
			Next:       "Keep one. Which one the router uses is not defined, so a later edit to one of them may not take effect.",
		}
		// The transit time is not part of the key — the router cannot tell the
		// connections apart either way — but "keep one" would silently drop one of
		// two transit configurations, so say that they differ.
		if transits := distinctTransits(edges[key]); len(transits) > 1 {
			f.Detail += fmt.Sprintf(", with different transit times (%s)", strings.Join(transits, "; "))
			f.Evidence["transit"] = transits
			f.Next = "Keep one, the one with the right transit time. Which one the router uses is not defined, so a later edit to one of them may not take effect."
		}
		out = append(out, f)
	}
	return out
}

// distinctTransits lists the different transit times of edges, as "1-2 days"
// in the order the edges come, "none" for a connection without one.
func distinctTransits(edges []*Edge) []string {
	var out []string
	for _, e := range edges {
		t := "none"
		if e.Transit != nil {
			t = fmt.Sprintf("%g-%g days", e.Transit.Min, e.Transit.Max)
		}
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// edgeKey is what makes two connections the same connection.
func (n *Network) edgeKey(e *Edge) string {
	sorted := func(v []string) string {
		v = slices.Clone(v)
		slices.Sort(v)
		return strings.Join(v, ",")
	}
	entries := make([]string, len(e.Context))
	for i, c := range e.Context {
		entries[i] = c.Type + " " + c.Operator + " " + sorted(n.Values(c))
	}
	slices.Sort(entries)
	return strings.Join([]string{e.Source, e.Target.Type, e.Target.FacilityRef,
		sorted(e.Target.ExcludedFacilities), sorted(e.Target.ExcludedGroups),
		strings.Join(entries, ";")}, "\x00")
}

func (n *Network) has(ref string) bool {
	_, ok := n.refs[ref]
	return ok
}

// label names a facility reference for a person: its tenant id when the
// snapshot knows it, the reference itself otherwise.
func (n *Network) label(ref string) string {
	if i, ok := n.refs[ref]; ok {
		return n.Facility(i).Label()
	}
	return ref
}

// maxEvidence caps the connection ids a finding lists. A dangling group can be
// named by thousands of connections; the count is in the detail.
const maxEvidence = 10

func capped(ids []string) []string {
	if len(ids) <= maxEvidence {
		return ids
	}
	return ids[:maxEvidence]
}

func verb(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	// facility → facilities; the nouns here are all regular otherwise.
	if stem, ok := strings.CutSuffix(noun, "y"); ok && !strings.ContainsAny(stem[len(stem)-1:], "aeiou") {
		return fmt.Sprintf("%d %sies", n, stem)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func sortedKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func sortedStrings[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// suppressed reports whether a suppression in force accepts f: same rule (or
// expectation name), same subject.
func (n *Network) suppressed(cfg Config, f Finding) bool {
	for _, s := range cfg.Suppress {
		rule := s.Rule == f.Rule || (f.Rule == RuleExpect && f.Evidence["expectation"] == s.Rule)
		if !rule {
			continue
		}
		if s.Facility == f.Subject {
			return true
		}
		if i, ok := n.lookupAny(s.Facility); ok && n.Facility(i).Label() == f.Subject {
			return true
		}
	}
	return false
}

// roleSet is the facilities a check decided play role.
func (r Report) roleSet(role string) Bitset {
	switch Kind(role) {
	case KindHub:
		return r.Roles.Hubs
	case KindSupplier:
		return r.Roles.Suppliers
	default:
		return r.Roles.Stores
	}
}

// expectations checks each declared expectation: every facility it is about
// is reached by at least AtLeast distinct sources of its role, for every
// order profile.
func (n *Network) expectations(cfg Config, r Report) []Finding {
	var out []Finding
	for _, exp := range cfg.Expect {
		targets := r.roleSet(exp.For.Role).Clone()
		targets.And(n.Select(&Selector{Where: exp.For.Where}))
		sources := r.roleSet(exp.ReachableFrom.Role)
		counts := n.sourceCounts(sources, targets, r.Profiles, exp.ReachableFrom.MaxTransitDays)

		type short struct {
			profiles []string
			least    int
		}
		shorts := map[int]*short{}
		for p, pc := range r.Profiles {
			targets.Each(func(i int) {
				have := counts[p][i]
				if have >= exp.ReachableFrom.AtLeast {
					return
				}
				s, ok := shorts[i]
				if !ok {
					s = &short{least: have}
					shorts[i] = s
				}
				s.profiles = append(s.profiles, pc.Profile.String())
				s.least = min(s.least, have)
			})
		}

		within := ""
		if d := exp.ReachableFrom.MaxTransitDays; d != nil {
			within = fmt.Sprintf(" within %g days", *d)
		}
		for _, i := range sortedKeys(shorts) {
			s := shorts[i]
			f := n.Facility(i)
			out = append(out, Finding{
				Rule:       RuleExpect,
				Subject:    f.Label(),
				Facilities: []string{f.Label()},
				Profiles:   s.profiles,
				Detail: fmt.Sprintf("%s: %s %s it%s, and at least %d should",
					exp.Name, plural(s.least, exp.ReachableFrom.Role), verb(s.least, "reaches", "reach"),
					within, exp.ReachableFrom.AtLeast),
				Evidence: map[string]any{"expectation": exp.Name, "have": s.least,
					"want": exp.ReachableFrom.AtLeast, "profiles": len(s.profiles), "of": len(r.Profiles)},
				Next: fmt.Sprintf("Find the %ss whose connections should reach it and the context entry or "+
					"exclusion that leaves it out — or, if the expectation is wrong for it, narrow its 'for'.",
					exp.ReachableFrom.Role),
			})
		}
	}
	return out
}

// sourceCounts is, per profile and per target, how many distinct sources reach
// it through connections that apply to the profile (and, with maxDays, arrive
// in time). It works source by source, so the memory it needs is one set per
// distinct context signature of one source, whatever the network's size.
func (n *Network) sourceCounts(sources, targets Bitset, profiles []ProfileCount, maxDays *float64) [][]int {
	counts := make([][]int, len(profiles))
	for p := range counts {
		counts[p] = make([]int, n.Len())
	}

	bySource := map[int][]int{}
	for k := range n.snap.Edges {
		e := &n.snap.Edges[k]
		src, ok := n.refs[e.Source]
		if !ok || !sources.Has(src) || e.Target.Type == TypeCustomer {
			continue
		}
		if maxDays != nil && (e.Transit == nil || e.Transit.Max > *maxDays) {
			continue
		}
		bySource[src] = append(bySource[src], k)
	}

	buf := n.empty()
	for _, src := range sortedKeys(bySource) {
		type sig struct {
			rep  Edge
			mask Bitset
		}
		sigs := map[string]*sig{}
		for _, k := range bySource[src] {
			e := &n.snap.Edges[k]
			var key strings.Builder
			var dependent []ContextEntry
			for _, c := range e.Context {
				if knownOperator(c.Operator) && (c.Type == ContextFacility || c.Type == ContextFacilityGroup) {
					continue
				}
				dependent = append(dependent, c)
				fmt.Fprintf(&key, "|%s %s %d", c.Type, c.Operator, c.Values)
			}
			g, ok := sigs[key.String()]
			if !ok {
				g = &sig{rep: Edge{Context: dependent}, mask: n.empty()}
				sigs[key.String()] = g
			}
			g.mask.Or(n.StaticTargets(e))
		}
		for p, pc := range profiles {
			clear(buf)
			for _, g := range sigs {
				if v, _ := n.Applies(&g.rep, pc.Profile); v == Applies {
					buf.Or(g.mask)
				}
			}
			buf.And(targets)
			buf.Each(func(i int) { counts[p][i]++ })
		}
	}
	return counts
}

// drift compares what the config pins with what the heuristics see now. Each
// difference is a question for the team — has the network changed, or was the
// pin wrong? — and the config file is where the answer is written down.
func (n *Network) drift(cfg Config, r Report) []Finding {
	var out []Finding
	add := func(subject, detail, next string, evidence map[string]any) {
		out = append(out, Finding{Rule: RuleConfigDrift, Subject: subject, Detail: detail, Next: next, Evidence: evidence})
	}

	if cfg.pinnedHubs() {
		candidates := n.Select(&Selector{Type: TypeManaged})
		candidates.And(n.online)
		inferred, cut, _ := n.relativeHubs(candidates, DefaultConfig().HubRatio)

		for _, ref := range cfg.Hub.Facilities {
			i, ok := n.lookupAny(ref)
			switch {
			case !ok:
				add(ref, "pinned as a hub, and not a facility in the snapshot",
					"Remove it from roles.hub, or correct the id.", map[string]any{"pinned": ref})
			case !n.Facility(i).Online():
				add(n.Facility(i).Label(), fmt.Sprintf("pinned as a hub, and %s", strings.ToLower(n.Facility(i).Status)),
					"Remove it from roles.hub if it is gone for good.", map[string]any{"status": n.Facility(i).Status})
			}
		}
		r.Roles.Hubs.Each(func(i int) {
			if !inferred.Has(i) {
				reach := n.reachOf(i)
				add(n.Facility(i).Label(),
					fmt.Sprintf("pinned as a hub, but its connections reach %s, which the heuristics would not call a hub", plural(reach, "facility")),
					"If it stopped being a hub, take it out of roles.hub; its stores are then judged without it.",
					map[string]any{"reach": reach, "heuristicCut": cut})
			}
		})
		inferred.Each(func(i int) {
			if !r.Roles.Hubs.Has(i) {
				reach := n.reachOf(i)
				add(n.Facility(i).Label(),
					fmt.Sprintf("its connections reach %s, as a hub's do, but roles.hub does not pin it", plural(reach, "facility")),
					"If it has become a hub, add it to roles.hub; until then its stores are not counted as hub-supplied by it.",
					map[string]any{"reach": reach})
			}
		})
	}

	if len(cfg.Declared) > 0 {
		declared := map[string]bool{}
		for _, pc := range r.Profiles {
			declared[pc.Profile.String()] = true
		}
		for _, pc := range n.InferProfiles(Config{}) {
			if pc.Orders > 0 && !declared[pc.Profile.String()] {
				add(pc.Profile.String(),
					fmt.Sprintf("%s routed recently carried this profile, which profiles.declared does not list", plural(pc.Orders, "order")),
					"Declare it, so the check covers the orders that actually arrive.",
					map[string]any{"orders": pc.Orders})
			}
		}
	}
	return out
}
