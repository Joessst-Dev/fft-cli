package network

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Config is what a check assumes about the network's shape, and what the team
// has declared about it. Every field has a default that worked on real
// networks; a config file ([LoadedFile.Settings]) is how a team pins them.
type Config struct {
	// HubRatio is how far a hub must stand out: the facilities above the largest
	// jump in reach between consecutive facilities that both reach somebody are
	// hubs, if that jump is at least this factor. (The drop to facilities that
	// reach nobody counts only when no such jump does, as for a lone hub over
	// stores that supply nothing.) A fixed count would not survive the move from a
	// pre-production network of a few dozen stores to a production one of
	// thousands.
	HubRatio float64
	// PeerBy is the facility field stores are grouped by.
	PeerBy string
	// PeerMinSize is the smallest peer group a store is compared with. Below it,
	// "most of its peers" is one or two facilities, and an outlier is noise.
	PeerMinSize int
	// PeerShare is how many of its peers must have a kind of source before a
	// store without one is reported.
	PeerShare float64
	// Rules selects the rules to run; empty runs all of them.
	Rules []string

	// FromFile says the settings came from a config file, which is what makes
	// config-drift worth checking: without one there is nothing pinned to drift
	// from.
	FromFile bool
	// Hub, Store and Supplier pick the roles; nil is the default for each.
	Hub, Store, Supplier *Selector
	// Declared replaces the inferred order profiles, when set.
	Declared [][]TagValue
	Expect   []Expectation
	// Suppress are the suppressions still in force.
	Suppress []Suppression
}

// DefaultConfig is what a check assumes when nobody has said otherwise.
func DefaultConfig() Config {
	return Config{HubRatio: 5, PeerBy: "address.country", PeerMinSize: 10, PeerShare: 0.9}
}

// pinnedHubs reports whether the hub selector names the hubs rather than
// asking for them to be found by reach.
func (c Config) pinnedHubs() bool {
	h := c.Hub
	return h != nil && h.OutDegree == "" && (len(h.Facilities) > 0 || len(h.Where) > 0 || h.Type != "")
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
// Unless the config pins them, a hub is a managed facility whose connections
// reach far more facilities than anybody else's. "Far more" is relative: the
// facilities are sorted by reach, and the cut goes at the largest jump between
// neighbours that both reach somebody, provided it is at least cfg.HubRatio. The
// drop to the facilities that reach nobody counts only when no such jump does.
// Reach counts facilities, not connections, because a wildcard target is one
// connection that reaches
// thousands.
func (n *Network) InferRoles(cfg Config) Roles {
	r := Roles{Stores: n.empty(), Suppliers: n.empty()}

	if cfg.pinnedHubs() {
		r.Hubs = n.Select(cfg.Hub)
		r.Hubs.And(n.online)
	} else {
		candidates := n.Select(&Selector{Type: TypeManaged})
		if h := cfg.Hub; h != nil {
			candidates.And(n.Select(&Selector{Type: h.Type, Where: h.Where}))
		}
		candidates.And(n.online)
		r.Hubs, r.HubCut, r.HubJump = n.relativeHubs(candidates, cfg.HubRatio)
	}

	stores := n.Select(cmp.Or(cfg.Store, &Selector{Type: TypeManaged}))
	suppliers := n.Select(cmp.Or(cfg.Supplier, &Selector{Type: TypeSupplier}))
	for i := range n.snap.Facilities {
		switch {
		case !n.online.Has(i) || r.Hubs.Has(i):
		case stores.Has(i):
			r.Stores.Set(i)
		case suppliers.Has(i):
			r.Suppliers.Set(i)
		}
	}
	return r
}

// relativeHubs picks, among candidates, the facilities whose reach stands
// ratio times above the next one's.
func (n *Network) relativeHubs(candidates Bitset, ratio float64) (Bitset, int, float64) {
	hubs := n.empty()

	// One source at a time, through one buffer: a set per source would cost
	// hundreds of megabytes on a network of tens of thousands of stores.
	order := make([]int, 0, len(n.snap.Edges))
	for i, e := range n.snap.Edges {
		if src, ok := n.refs[e.Source]; ok && e.Target.Type == TypeManaged && candidates.Has(src) {
			order = append(order, i)
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
	// The candidates that reach nobody rank too, at the bottom: a network where
	// one facility supplies every store and the stores supply nothing has a hub,
	// and there would be no neighbour to measure it against without them.
	seen := map[int]bool{}
	for _, rk := range ranks {
		seen[rk.index] = true
	}
	candidates.Each(func(i int) {
		if !seen[i] {
			ranks = append(ranks, ranked{i, 0})
		}
	})
	slices.SortFunc(ranks, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(b.reach, a.reach), cmp.Compare(a.index, b.index))
	})

	// The cut goes at the largest jump between neighbours that reach somebody. The
	// drop to the facilities that reach nobody is a candidate only when no other
	// jump stands out: measured against a denominator of 1 it equals the last
	// tier's own reach, which would beat a real hub tier above a small
	// ship-from-store tier whenever that reach exceeds the hubs' jump over it.
	best, at := 0.0, -1
	last := -1
	for k := 0; k < len(ranks) && ranks[k].reach > 0; k++ {
		last = k
		if k+1 < len(ranks) && ranks[k+1].reach > 0 {
			if jump := float64(ranks[k].reach) / float64(ranks[k+1].reach); jump > best {
				best, at = jump, k
			}
		}
	}
	if best < ratio && last >= 0 && last+1 < len(ranks) {
		best, at = float64(ranks[last].reach), last
	}
	if at < 0 || best < ratio {
		return hubs, 0, 0
	}
	for _, rk := range ranks[:at+1] {
		hubs.Set(rk.index)
	}
	return hubs, ranks[at].reach, best
}

// reachOf counts the online facilities the connections of the facility at i
// reach, for config-drift to say why a pinned hub no longer looks like one.
func (n *Network) reachOf(i int) int {
	buf := n.empty()
	for k := range n.snap.Edges {
		e := &n.snap.Edges[k]
		if j, ok := n.refs[e.Source]; ok && j == i && e.Target.Type == TypeManaged {
			buf.Or(n.StaticTargets(e))
		}
	}
	buf.And(n.online)
	return buf.Count()
}

// PeerGroup is stores compared with one another: those with one value of the
// peer field, by default the country.
type PeerGroup struct {
	Key     string `json:"key"`
	Size    int    `json:"size"`
	members Bitset
}

// noValue keys the stores that have no value for the peer field.
const noValue = "(none)"

// InferPeers groups the stores by cfg.PeerBy, largest group first.
func (n *Network) InferPeers(cfg Config, roles Roles) []PeerGroup {
	by := cmp.Or(cfg.PeerBy, DefaultConfig().PeerBy)
	byKey := map[string]Bitset{}
	roles.Stores.Each(func(i int) {
		key := cmp.Or(n.field(i, by), noValue)
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
func (n *Network) InferProfiles(cfg Config) []ProfileCount {
	if len(cfg.Declared) > 0 {
		out := make([]ProfileCount, len(cfg.Declared))
		for i, tags := range cfg.Declared {
			out[i] = ProfileCount{Profile: NewProfile(tags)}
		}
		return out
	}

	used := map[string]bool{}
	var positive []string
	for _, e := range n.snap.Edges {
		for _, c := range e.Context {
			if c.Type != ContextTag {
				continue
			}
			for _, v := range n.Values(c) {
				// Independent of used: a NOT entry met first must not hide a
				// positive entry for the same value, or the profiles would follow
				// the crawl order. The sort and Compact below deduplicate.
				used[v] = true
				if c.Operator == "" {
					positive = append(positive, v)
				}
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
