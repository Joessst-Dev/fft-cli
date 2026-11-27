package network_test

import (
	"slices"

	"github.com/Joessst-Dev/fft-cli/internal/network"
)

// The specs' network: one hub supplying two Berlin stores and a supplier, with a
// group holding one of the stores. Every id is a placeholder.
const (
	hubID  = "8f14e45f-ceea-467a-9575-25a1b5c8b3a1"
	ber1ID = "c9f0f895-fb98-4b91-9c5b-3d2f1a0e7b11"
	ber2ID = "45c48cce-2e2d-4fbd-8e7c-6d1f0a3b9c22"
	supID  = "d3d94468-02a4-4b3b-9e1d-0c6a5f7e8d33"
	offID  = "6512bd43-d9ca-4a6e-8f0b-2c1d3e4f5a44"

	berlinGroup = "e2c420d9-28f1-4d6b-9a7c-1b2c3d4e5f55"
)

func facilities() []network.Facility {
	return []network.Facility{
		{ID: hubID, TenantFacilityID: "HUB-01", Type: network.TypeManaged, Status: network.StatusOnline, Country: "DE"},
		{ID: ber1ID, TenantFacilityID: "BER-01", Type: network.TypeManaged, Status: network.StatusOnline, Country: "DE"},
		{ID: ber2ID, TenantFacilityID: "BER-02", Type: network.TypeManaged, Status: network.StatusOnline, Country: "DE"},
		{ID: supID, TenantFacilityID: "SUP-01", Type: network.TypeSupplier, Status: network.StatusOnline, Country: "DE"},
	}
}

// entry is a context entry as a spec writes it: with its values inline.
type entry struct {
	typ    string
	not    bool
	values []string
}

func in(typ string, values ...string) entry  { return entry{typ: typ, values: values} }
func not(typ string, values ...string) entry { return entry{typ: typ, not: true, values: values} }

// spec is a connection as a spec writes it.
type spec struct {
	target  network.Target
	context []entry
}

func wildcard(typ string) network.Target { return network.Target{Type: typ} }
func to(ref string) network.Target {
	return network.Target{Type: network.TypeManaged, FacilityRef: ref}
}

// snapshot builds a snapshot of the specs' facilities with one edge per spec,
// each from the hub, interning the context values as a crawl would.
func snapshot(specs ...spec) *network.Snapshot {
	s := &network.Snapshot{
		SchemaVersion: network.SchemaVersion,
		Facilities:    facilities(),
		Groups:        []network.Group{{ID: berlinGroup, Members: []string{ber1ID}}},
	}
	for i, sp := range specs {
		e := network.Edge{ID: string(rune('a' + i)), Source: hubID, Target: sp.target}
		for _, c := range sp.context {
			idx := slices.IndexFunc(s.ValueSets, func(v []string) bool { return slices.Equal(v, c.values) })
			if idx < 0 {
				idx = len(s.ValueSets)
				s.ValueSets = append(s.ValueSets, c.values)
			}
			op := ""
			if c.not {
				op = network.OperatorNot
			}
			e.Context = append(e.Context, network.ContextEntry{Type: c.typ, Operator: op, Values: idx})
		}
		s.Edges = append(s.Edges, e)
	}
	return s
}

func profile(values ...string) network.Profile {
	tags := make([]network.TagValue, len(values))
	for i, v := range values {
		tags[i] = network.TagValue{ID: "orderType", Value: v}
	}
	return network.NewProfile(tags)
}

// labels names the members of a set by their tenant facility ids.
func labels(n *network.Network, set network.Bitset) []string {
	out := []string{}
	set.Each(func(i int) { out = append(out, n.Facility(i).Label()) })
	return out
}
