package network

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Config is what a check assumes about the network's shape. Every field has a
// default that worked on real networks; `fft network infer` (and the config
// file it writes) is how a team pins them instead.
type Config struct {
	// HubRatio is how far a hub must stand out: the facilities above the largest
	// jump in reach between consecutive facilities are hubs, if that jump is at
	// least this factor. A fixed count would not survive the move from a
	// pre-production network of a few dozen stores to a production one of
	// thousands.
	HubRatio float64
	// PeerMinSize is the smallest peer group a store is compared with. Below it,
	// "most of its peers" is one or two facilities, and an outlier is noise.
	PeerMinSize int
	// PeerShare is how many of its peers must have a kind of source before a
	// store without one is reported.
	PeerShare float64
	// Rules selects the rules to run; empty runs all of them.
	Rules []string
}

// DefaultConfig is what a check assumes when nobody has said otherwise.
func DefaultConfig() Config {
	return Config{HubRatio: 5, PeerMinSize: 10, PeerShare: 0.9}
}

// Kind is the role a facility plays as a source.
type Kind string

// The roles.
const (
	KindHub      Kind = "hub"
	KindStore    Kind = "store"
	KindSupplier Kind = "supplier"
)

// Roles is what a check decided each facility is.
type Roles struct {
	Hubs, Stores, Suppliers Bitset
	// HubCut is the reach a facility needed to count as a hub, and HubJump the
	// factor between it and the next facility down; both zero when the network
	// has no hub tier.
	HubCut  int
	HubJump float64
}

// kind is the role of the facility at index i as a source, and whether it can
// be one at all: an offline facility sources nothing.
func (r Roles) kind(i int) (Kind, bool) {
	switch {
	case r.Hubs.Has(i):
		return KindHub, true
	case r.Stores.Has(i):
		return KindStore, true
	case r.Suppliers.Has(i):
		return KindSupplier, true
	}
	return "", false
}

// InferRoles decides which online facilities are hubs, stores and suppliers.
//
// A hub is a managed facility whose connections reach far more managed
// facilities than anybody else's. "Far more" is relative: the facilities are
// sorted by reach, and the cut goes at the largest jump between neighbours,
// provided it is at least cfg.HubRatio. Reach counts facilities, not
// connections, because a wildcard target is one connection that reaches
// thousands.
func (n *Network) InferRoles(cfg Config) Roles {
	r := Roles{Hubs: n.empty(), Stores: n.empty(), Suppliers: n.empty()}

	// One source at a time, through one buffer: a set per source would cost
	// hundreds of megabytes on a network of tens of thousands of stores.
	order := make([]int, 0, len(n.snap.Edges))
	for i, e := range n.snap.Edges {
		if src, ok := n.refs[e.Source]; ok && e.Target.Type == TypeManaged {
			if f := n.Facility(src); f.Type == TypeManaged && f.Online() {
				order = append(order, i)
			}
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return strings.Compare(n.snap.Edges[a].Source, n.snap.Edges[b].Source)
	})

	type ranked struct{ index, reach int }
	var ranks []ranked
	buf := n.empty()
	for k, i := range order {
		e := &n.snap.Edges[i]
		buf.Or(n.StaticTargets(e))
		if k+1 == len(order) || n.snap.Edges[order[k+1]].Source != e.Source {
			// Only online facilities count: an edge to a closed store reaches nobody.
			buf.And(n.online)
			ranks = append(ranks, ranked{n.refs[e.Source], buf.Count()})
			clear(buf)
		}
	}
	// The managed facilities that reach nobody rank too, at the bottom: a network
	// where one facility supplies every store and the stores supply nothing has a
	// hub, and there would be no neighbour to measure it against without them.
	seen := map[int]bool{}
	for _, rk := range ranks {
		seen[rk.index] = true
	}
	for i, f := range n.snap.Facilities {
		if f.Type == TypeManaged && f.Online() && !seen[i] {
			ranks = append(ranks, ranked{i, 0})
		}
	}
	slices.SortFunc(ranks, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(b.reach, a.reach), cmp.Compare(a.index, b.index))
	})

	best, at := 0.0, -1
	for k := 0; k+1 < len(ranks) && ranks[k].reach > 0; k++ {
		if jump := float64(ranks[k].reach) / float64(max(ranks[k+1].reach, 1)); jump > best {
			best, at = jump, k
		}
	}
	if at >= 0 && best >= cfg.HubRatio {
		r.HubCut, r.HubJump = ranks[at].reach, best
		for _, rk := range ranks[:at+1] {
			r.Hubs.Set(rk.index)
		}
	}

	for i, f := range n.snap.Facilities {
		switch {
		case !f.Online() || r.Hubs.Has(i):
		case f.Type == TypeManaged:
			r.Stores.Set(i)
		case f.Type == TypeSupplier:
			r.Suppliers.Set(i)
		}
	}
	return r
}

// PeerGroup is stores compared with one another: those in one country.
type PeerGroup struct {
	Key     string `json:"key"`
	Size    int    `json:"size"`
	members Bitset
}

// noCountry keys the stores whose address names no country.
const noCountry = "(no country)"

// InferPeers groups the stores by their address's country, largest first.
func (n *Network) InferPeers(roles Roles) []PeerGroup {
	byKey := map[string]Bitset{}
	roles.Stores.Each(func(i int) {
		key := cmp.Or(n.Facility(i).Country, noCountry)
		set, ok := byKey[key]
		if !ok {
			set = n.empty()
			byKey[key] = set
		}
		set.Set(i)
	})
	groups := make([]PeerGroup, 0, len(byKey))
	for key, set := range byKey {
		groups = append(groups, PeerGroup{Key: key, Size: set.Count(), members: set})
	}
	slices.SortFunc(groups, func(a, b PeerGroup) int {
		return cmp.Or(cmp.Compare(b.Size, a.Size), strings.Compare(a.Key, b.Key))
	})
	return groups
}

// ProfileCount is an order profile and how many sampled orders carried it.
type ProfileCount struct {
	Profile Profile
	Orders  int
}

// InferProfiles is the kinds of order a check compares, most common first.
//
// They come from the orders the snapshot's decision logs routed, reduced to the
// tag values some connection's context mentions: a tag no connection reads
// cannot change where an order may go, and keeping it would split one kind of
// order into many. A snapshot without routed orders falls back to one profile
// per tag value a positive context entry asks for.
func (n *Network) InferProfiles() []ProfileCount {
	used := map[string]bool{}
	var positive []string
	for _, e := range n.snap.Edges {
		for _, c := range e.Context {
			if c.Type != ContextTag {
				continue
			}
			for _, v := range n.Values(c) {
				if !used[v] && c.Operator == "" {
					positive = append(positive, v)
				}
				used[v] = true
			}
		}
	}

	counts := map[string]*ProfileCount{}
	var order []string
	for _, op := range n.snap.OrderProfiles {
		var tags []TagValue
		for _, t := range op.Tags {
			if used[t.Value] {
				tags = append(tags, t)
			}
		}
		p := NewProfile(tags)
		key := p.String()
		if c, ok := counts[key]; ok {
			c.Orders += op.Orders
			continue
		}
		counts[key] = &ProfileCount{Profile: p, Orders: op.Orders}
		order = append(order, key)
	}

	if len(order) == 0 {
		slices.Sort(positive)
		for _, v := range slices.Compact(positive) {
			p := NewProfile([]TagValue{{ID: n.tagOf(v), Value: v}})
			counts[p.String()] = &ProfileCount{Profile: p}
			order = append(order, p.String())
		}
	}
	if len(order) == 0 {
		p := NewProfile(nil)
		counts[p.String()] = &ProfileCount{Profile: p}
		order = append(order, p.String())
	}

	out := make([]ProfileCount, 0, len(order))
	for _, key := range order {
		out = append(out, *counts[key])
	}
	slices.SortStableFunc(out, func(a, b ProfileCount) int { return cmp.Compare(b.Orders, a.Orders) })
	return out
}

// tagOf names the tag a value is allowed for, or "tag" when no tag allows it —
// which the dangling-ref rule reports separately.
func (n *Network) tagOf(value string) string {
	for _, t := range n.snap.Tags {
		if slices.Contains(t.AllowedValues, value) {
			return t.ID
		}
	}
	return "tag"
}

// share renders have of size as a percentage.
func share(have, size int) string {
	if size == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", have*100/size)
}
