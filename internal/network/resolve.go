package network

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

// Network is a [Snapshot] indexed for resolving. Building one is linear in the
// snapshot; every answer after that works on bitsets.
//
// It memoises the facility set of each context value list it has resolved, so
// it is not safe for concurrent use.
type Network struct {
	snap   *Snapshot
	refs   map[string]int
	byType map[string]Bitset
	groups map[string]Bitset

	facilitySets map[int]Bitset
	groupSets    map[int]Bitset
	valueSets    map[int]map[string]bool
}

// New indexes s.
func New(s *Snapshot) *Network {
	n := &Network{
		snap:         s,
		refs:         make(map[string]int, 2*len(s.Facilities)),
		byType:       map[string]Bitset{},
		groups:       make(map[string]Bitset, len(s.Groups)),
		facilitySets: map[int]Bitset{},
		groupSets:    map[int]Bitset{},
		valueSets:    map[int]map[string]bool{},
	}
	for i, f := range s.Facilities {
		// A context value, a target and an exclusion may name a facility by its
		// platform id or by its tenant id's URN; both lead here.
		n.refs[f.ID] = i
		if f.TenantFacilityID != "" {
			n.refs[client.FacilityRef(f.TenantFacilityID)] = i
		}
		set, ok := n.byType[f.Type]
		if !ok {
			set = n.empty()
			n.byType[f.Type] = set
		}
		set.Set(i)
	}
	for _, g := range s.Groups {
		set := n.empty()
		for _, ref := range g.Members {
			if i, ok := n.refs[ref]; ok {
				set.Set(i)
			}
		}
		n.groups[g.ID] = set
		if g.TenantID != "" {
			n.groups[g.TenantID] = set
		}
	}
	return n
}

// Snapshot is the snapshot n indexes.
func (n *Network) Snapshot() *Snapshot { return n.snap }

// Len is the number of facilities, and so the bound of every index.
func (n *Network) Len() int { return len(n.snap.Facilities) }

// Facility is the facility at index i.
func (n *Network) Facility(i int) *Facility { return &n.snap.Facilities[i] }

// Lookup finds a facility by platform id or tenant-id URN.
func (n *Network) Lookup(ref string) (int, bool) {
	i, ok := n.refs[ref]
	return i, ok
}

// HasGroup reports whether a facility group exists.
func (n *Network) HasGroup(id string) bool {
	_, ok := n.groups[id]
	return ok
}

// Values is the value list a context entry points at.
func (n *Network) Values(c ContextEntry) []string { return n.snap.ValueSets[c.Values] }

func (n *Network) empty() Bitset { return NewBitset(len(n.snap.Facilities)) }

// Profile is a kind of order, as far as connections can tell orders apart: the
// tags it carries.
type Profile struct {
	Tags   []TagValue
	values map[string]bool
}

// NewProfile is the profile of an order carrying tags.
func NewProfile(tags []TagValue) Profile {
	p := Profile{Tags: slices.Clone(tags), values: make(map[string]bool, len(tags))}
	for _, t := range tags {
		p.values[t.Value] = true
	}
	return p
}

// String is the profile as id=value pairs, the form `fft network reach --tag`
// takes them in.
func (p Profile) String() string {
	if len(p.Tags) == 0 {
		return "(no tags)"
	}
	parts := make([]string, len(p.Tags))
	for i, t := range p.Tags {
		parts[i] = t.ID + "=" + t.Value
	}
	return strings.Join(parts, ",")
}

// matches reports whether the order carries any of values.
//
// A TAG_REFERENCE entry lists the tag's allowed values, and the router compares
// them with the values on the order. Which tag a value belongs to is a tenant's
// naming convention, not a rule, so the comparison is on the value alone.
func (p Profile) matches(values []string) (string, bool) {
	for _, v := range values {
		if p.values[v] {
			return v, true
		}
	}
	return "", false
}

// Verdict is whether a connection applies to an order profile.
type Verdict uint8

const (
	// Applies: the connection is usable for the profile, to the facilities in
	// [Reach.Targets].
	Applies Verdict = iota
	// Excluded: a context entry rules the profile out, whatever the facility.
	Excluded
	// Conditional: the connection applies only to some orders of the profile —
	// it depends on an item's category or the recipient's address, which a tag
	// profile does not say. It is never counted as coverage, and never as a gap.
	Conditional
)

func (v Verdict) String() string {
	switch v {
	case Applies:
		return "applies"
	case Excluded:
		return "excluded"
	default:
		return "conditional"
	}
}

// Reach is what a connection does for one order profile.
type Reach struct {
	// Targets are the facilities the connection leads to. Nil for a CUSTOMER
	// target, which leads to no facility.
	Targets Bitset
	Verdict Verdict
	// Entry is the index of the context entry that excluded the profile or made
	// the connection conditional, or -1.
	Entry int
}

// Reach resolves e for p: which facilities it leads to, if it applies at all.
//
// The rules are the router's, as connections-semantics.md records them: entries
// combine with AND and the values of one entry with OR; FACILITY and
// FACILITY_GROUP match the receiving facility; NOT means none of the values may
// match; a tag the order lacks passes NOT and fails a positive entry.
func (n *Network) Reach(e *Edge, p Profile) Reach {
	r := Reach{Verdict: Applies, Entry: -1}

	// The entries that depend on the order alone go first: they can rule the
	// whole connection out without building a set.
	for i, c := range e.Context {
		switch {
		case !knownOperator(c.Operator):
			r.conditional(i)
		case c.Type == ContextTag:
			_, hit := p.matches(n.Values(c))
			if hit == (c.Operator == OperatorNot) {
				return Reach{Verdict: Excluded, Entry: i}
			}
		case c.Type == ContextFacility, c.Type == ContextFacilityGroup:
		default:
			// CATEGORY and the recipient's address, and any type the API grows
			// later: true for some orders of this profile, false for others.
			r.conditional(i)
		}
	}

	if e.Target.Type == TypeCustomer {
		return r
	}

	targets := n.targets(e)
	for _, c := range e.Context {
		if !knownOperator(c.Operator) {
			continue
		}
		var set Bitset
		switch c.Type {
		case ContextFacility:
			set = n.facilitySet(c.Values)
		case ContextFacilityGroup:
			set = n.groupSet(c.Values)
		default:
			continue
		}
		if c.Operator == OperatorNot {
			targets.AndNot(set)
		} else {
			targets.And(set)
		}
	}
	r.Targets = targets
	return r
}

func (r *Reach) conditional(entry int) {
	if r.Verdict == Applies {
		r.Verdict, r.Entry = Conditional, entry
	}
}

func knownOperator(op string) bool { return op == "" || op == OperatorNot }

// targets is the set e's target names before its context narrows it: one
// facility, or every facility of the target type, less the exclusions. The
// source is never its own target.
func (n *Network) targets(e *Edge) Bitset {
	var set Bitset
	if e.Target.FacilityRef != "" {
		set = n.empty()
		if i, ok := n.refs[e.Target.FacilityRef]; ok {
			set.Set(i)
		}
	} else if all, ok := n.byType[e.Target.Type]; ok {
		set = all.Clone()
	} else {
		set = n.empty()
	}

	if i, ok := n.refs[e.Source]; ok {
		set.AndNot(single(n.Len(), i))
	}
	for _, ref := range e.Target.ExcludedFacilities {
		if i, ok := n.refs[ref]; ok {
			set.AndNot(single(n.Len(), i))
		}
	}
	for _, g := range e.Target.ExcludedGroups {
		set.AndNot(n.groups[g])
	}
	return set
}

func single(size, i int) Bitset {
	b := NewBitset(size)
	b.Set(i)
	return b
}

// facilitySet is the facilities a FACILITY value list names. A value that names
// no facility in the snapshot matches nothing; the dangling-ref rule reports it.
func (n *Network) facilitySet(values int) Bitset {
	if set, ok := n.facilitySets[values]; ok {
		return set
	}
	set := n.empty()
	for _, ref := range n.snap.ValueSets[values] {
		if i, ok := n.refs[ref]; ok {
			set.Set(i)
		}
	}
	n.facilitySets[values] = set
	return set
}

// groupSet is the members of every group a FACILITY_GROUP value list names.
func (n *Network) groupSet(values int) Bitset {
	if set, ok := n.groupSets[values]; ok {
		return set
	}
	set := n.empty()
	for _, g := range n.snap.ValueSets[values] {
		set.Or(n.groups[g])
	}
	n.groupSets[values] = set
	return set
}

// Reason is why a connection does or does not lead to one facility for one
// order profile.
type Reason struct {
	Applies     bool
	Conditional bool
	// Entry is the context entry the reason is about, or -1 when it is the
	// target's own definition.
	Entry int
	Why   string
}

// Explain says why e does or does not lead to the facility at index i for p.
// It names the first thing that rules the facility out, in the order the router
// would meet it: the target, its exclusions, then each context entry.
func (n *Network) Explain(e *Edge, i int, p Profile) Reason {
	f := n.Facility(i)
	out := func(entry int, format string, args ...any) Reason {
		return Reason{Entry: entry, Why: fmt.Sprintf(format, args...)}
	}

	if j, ok := n.refs[e.Source]; ok && j == i {
		return out(-1, "it is the connection's own source")
	}
	switch {
	case e.Target.Type == TypeCustomer:
		return out(-1, "the connection leads to the customer, not to a facility")
	case e.Target.FacilityRef != "":
		if j, ok := n.refs[e.Target.FacilityRef]; !ok || j != i {
			return out(-1, "the connection targets %s only", e.Target.FacilityRef)
		}
	case e.Target.Type != f.Type:
		return out(-1, "the connection targets %s facilities, and this is a %s", e.Target.Type, f.Type)
	}
	for _, ref := range e.Target.ExcludedFacilities {
		if j, ok := n.refs[ref]; ok && j == i {
			return out(-1, "the target's excludedFacilityRefs lists it")
		}
	}
	for _, g := range e.Target.ExcludedGroups {
		if n.groups[g].Has(i) {
			return out(-1, "the target's excludedFacilityGroupRefs lists group %s, which contains it", g)
		}
	}

	conditional := Reason{Entry: -1}
	for k, c := range e.Context {
		values := n.Values(c)
		not := c.Operator == OperatorNot
		switch {
		case !knownOperator(c.Operator):
			if conditional.Entry < 0 {
				conditional = out(k, "context[%d] has the operator %s, which fft cannot evaluate", k, c.Operator)
			}
		case c.Type == ContextTag:
			v, hit := p.matches(values)
			switch {
			case not && hit:
				return out(k, "context[%d] %s NOT: the order carries %s", k, c.Type, v)
			case !not && !hit:
				return out(k, "context[%d] %s: the order carries none of %s", k, c.Type, list(values))
			}
		case c.Type == ContextFacility || c.Type == ContextFacilityGroup:
			set := n.facilitySet(c.Values)
			if c.Type == ContextFacilityGroup {
				set = n.groupSet(c.Values)
			}
			switch in := set.Has(i); {
			case not && in:
				return out(k, "context[%d] %s NOT lists it", k, c.Type)
			case !not && !in:
				return out(k, "context[%d] %s does not list it", k, c.Type)
			}
		default:
			if conditional.Entry < 0 {
				conditional = out(k, "context[%d] %s depends on the order line or the recipient, not on tags", k, c.Type)
			}
		}
	}

	if conditional.Entry >= 0 {
		conditional.Applies, conditional.Conditional = true, true
		return conditional
	}
	return Reason{Applies: true, Entry: -1, Why: "every context entry passes"}
}

// list shortens a value list for a sentence. A FACILITY entry can list hundreds.
func list(values []string) string {
	const show = 3
	if len(values) <= show {
		return strings.Join(values, ", ")
	}
	return fmt.Sprintf("%s (and %d more)", strings.Join(values[:show], ", "), len(values)-show)
}
