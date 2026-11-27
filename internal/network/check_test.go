package network_test

import (
	"fmt"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/network"
)

// world builds a network for the check specs. Facilities are named by tenant id;
// their platform id is the name with "id-" in front.
type world struct{ s network.Snapshot }

func newWorld() *world { return &world{s: network.Snapshot{SchemaVersion: network.SchemaVersion}} }

func id(label string) string { return "id-" + label }

func (w *world) facility(label, typ, status, country string) *world {
	w.s.Facilities = append(w.s.Facilities, network.Facility{
		ID: id(label), TenantFacilityID: label, Type: typ, Status: status, Country: country})
	return w
}

func (w *world) hub(label string) *world {
	return w.facility(label, network.TypeManaged, network.StatusOnline, "DE")
}

func (w *world) supplier(label string) *world {
	return w.facility(label, network.TypeSupplier, network.StatusOnline, "DE")
}

// stores adds n online stores in country, named <prefix>-01, <prefix>-02, ...
func (w *world) stores(prefix, country string, n int) *world {
	for i := 1; i <= n; i++ {
		w.facility(fmt.Sprintf("%s-%02d", prefix, i), network.TypeManaged, network.StatusOnline, country)
	}
	return w
}

func (w *world) edge(edgeID, source string, target network.Target, context ...entry) *world {
	e := network.Edge{ID: edgeID, Source: id(source), Target: target}
	for _, c := range context {
		idx := slices.IndexFunc(w.s.ValueSets, func(v []string) bool { return slices.Equal(v, c.values) })
		if idx < 0 {
			idx = len(w.s.ValueSets)
			w.s.ValueSets = append(w.s.ValueSets, c.values)
		}
		op := ""
		if c.not {
			op = network.OperatorNot
		}
		e.Context = append(e.Context, network.ContextEntry{Type: c.typ, Operator: op, Values: idx})
	}
	w.s.Edges = append(w.s.Edges, e)
	return w
}

func (w *world) check(rules ...string) network.Report {
	cfg := network.DefaultConfig()
	cfg.Rules = rules
	return network.New(&w.s).Check(cfg)
}

func except(typ string, labels ...string) network.Target {
	t := network.Target{Type: typ}
	for _, l := range labels {
		t.ExcludedFacilities = append(t.ExcludedFacilities, id(l))
	}
	return t
}

func subjects(r network.Report, rule string) []string {
	out := []string{}
	for _, f := range r.Findings {
		if f.Rule == rule {
			out = append(out, f.Subject)
		}
	}
	return out
}

func finding(r network.Report, rule, subject string) network.Finding {
	GinkgoHelper()
	for _, f := range r.Findings {
		if f.Rule == rule && f.Subject == subject {
			return f
		}
	}
	Fail(fmt.Sprintf("no %s finding for %s in %v", rule, subject, r.Findings))
	return network.Finding{}
}

// germany is two hubs and a supplier serving eleven stores. HUB-01 reaches
// every store; HUB-02 every store but BER-11; the supplier every store.
func germany() *world {
	return newWorld().hub("HUB-01").hub("HUB-02").supplier("SUP-01").stores("BER", "DE", 11).
		edge("h1", "HUB-01", network.Target{Type: network.TypeManaged}).
		edge("h2", "HUB-02", except(network.TypeManaged, "BER-11")).
		edge("s1", "SUP-01", network.Target{Type: network.TypeManaged}).
		edge("st", "BER-01", network.Target{Type: network.TypeManaged, FacilityRef: id("BER-02")})
}

var _ = Describe("checking a network", func() {
	Describe("the roles it infers", func() {
		It("takes the facilities whose reach stands far above the rest as hubs", func() {
			r := germany().check()

			n := network.New(&germany().s)
			hubs := []string{}
			r.Roles.Hubs.Each(func(i int) { hubs = append(hubs, n.Facility(i).Label()) })
			Expect(hubs).To(Equal([]string{"HUB-01", "HUB-02"}))
			Expect(r.Roles.Stores.Count()).To(Equal(11))
			Expect(r.Roles.Suppliers.Count()).To(Equal(1))
			Expect(r.Roles.HubCut).To(Equal(11))
		})

		It("infers no hub tier when no facility stands out", func() {
			r := newWorld().stores("BER", "DE", 3).
				edge("a", "BER-01", network.Target{Type: network.TypeManaged, FacilityRef: id("BER-02")}).
				edge("b", "BER-02", network.Target{Type: network.TypeManaged, FacilityRef: id("BER-03")}).
				check()

			Expect(r.Roles.Hubs.Empty()).To(BeTrue())
			Expect(r.Roles.Stores.Count()).To(Equal(3))
		})

		It("does not take ship-from-store stores for hubs because they stand above stores that reach nobody", func() {
			// A real hub tier (reach 41) above four stores that each ship to 8 neighbours
			// above stores that reach nobody. Measured against the zero-reach tier, the
			// stores' own drop (8/1) would beat the hubs' (41/8).
			w := newWorld().hub("HUB-01").hub("HUB-02").stores("BER", "DE", 40).
				edge("h1", "HUB-01", network.Target{Type: network.TypeManaged}).
				edge("h2", "HUB-02", network.Target{Type: network.TypeManaged})
			for s := 1; s <= 4; s++ {
				for k := 0; k < 8; k++ {
					w.edge(fmt.Sprintf("s%d-%d", s, k), fmt.Sprintf("BER-%02d", s),
						network.Target{Type: network.TypeManaged, FacilityRef: id(fmt.Sprintf("BER-%02d", 10+s*8+k))})
				}
			}

			r := w.check()

			n := network.New(&w.s)
			hubs := []string{}
			r.Roles.Hubs.Each(func(i int) { hubs = append(hubs, n.Facility(i).Label()) })
			Expect(hubs).To(Equal([]string{"HUB-01", "HUB-02"}))
			Expect(r.Roles.Stores.Count()).To(Equal(40))
		})

		It("groups the stores by country, largest group first", func() {
			r := germany().stores("VIE", "AT", 2).check()

			Expect(r.Peers).To(HaveLen(2))
			Expect(r.Peers[0].Key).To(Equal("DE"))
			Expect(r.Peers[0].Size).To(Equal(11))
			Expect(r.Peers[1].Key).To(Equal("AT"))
		})
	})

	Describe("the order profiles it compares", func() {
		It("takes them from the routed orders, keeping only the tags a connection reads", func() {
			w := germany().edge("x", "HUB-01", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "express"))
			w.s.OrderProfiles = []network.OrderProfile{
				{Tags: []network.TagValue{{ID: "orderType", Value: "express"}, {ID: "channel", Value: "web"}}, Orders: 3},
				{Tags: []network.TagValue{{ID: "orderType", Value: "express"}, {ID: "channel", Value: "app"}}, Orders: 2},
				{Tags: []network.TagValue{{ID: "channel", Value: "web"}}, Orders: 4},
			}

			ps := network.New(&w.s).InferProfiles(network.DefaultConfig())

			Expect(ps).To(HaveLen(2))
			Expect(ps[0].Profile.String()).To(Equal("orderType=express"))
			Expect(ps[0].Orders).To(Equal(5))
			Expect(ps[1].Profile.String()).To(Equal("(no tags)"))
			Expect(ps[1].Orders).To(Equal(4))
		})

		It("falls back to one profile per tag value a connection asks for", func() {
			w := germany().
				edge("x", "HUB-01", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "sameday", "express")).
				edge("y", "HUB-02", network.Target{Type: network.TypeManaged}, not(network.ContextTag, "bulky"))
			w.s.Tags = []network.Tag{{ID: "orderType", AllowedValues: []string{"express", "sameday", "bulky"}}}

			ps := network.New(&w.s).InferProfiles(network.DefaultConfig())

			names := []string{}
			for _, p := range ps {
				names = append(names, p.Profile.String())
			}
			Expect(names).To(Equal([]string{"orderType=express", "orderType=sameday"}))
		})

		It("does not let a NOT entry met first hide the positive entry for the same value", func() {
			// Crawl order is not meaningful: the same network must give the same profiles.
			names := func(w *world) []string {
				out := []string{}
				for _, p := range network.New(&w.s).InferProfiles(network.DefaultConfig()) {
					out = append(out, p.Profile.String())
				}
				return out
			}
			notFirst := germany().
				edge("a", "HUB-01", network.Target{Type: network.TypeManaged}, not(network.ContextTag, "express")).
				edge("b", "HUB-02", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "express"))
			positiveFirst := germany().
				edge("b", "HUB-02", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "express")).
				edge("a", "HUB-01", network.Target{Type: network.TypeManaged}, not(network.ContextTag, "express"))
			notFirst.s.Tags = []network.Tag{{ID: "orderType", AllowedValues: []string{"express"}}}
			positiveFirst.s.Tags = notFirst.s.Tags

			Expect(names(notFirst)).To(Equal([]string{"orderType=express"}))
			Expect(names(positiveFirst)).To(Equal(names(notFirst)))
		})
	})

	Describe("no-source", func() {
		It("reports a store no online source reaches, for the profiles it holds for", func() {
			w := newWorld().hub("HUB-01").stores("BER", "DE", 6).
				edge("h1", "HUB-01", except(network.TypeManaged, "BER-06"))

			r := w.check(network.RuleNoSource)

			Expect(subjects(r, network.RuleNoSource)).To(Equal([]string{"BER-06"}))
			f := finding(r, network.RuleNoSource, "BER-06")
			Expect(f.Profiles).To(Equal([]string{"(no tags)"}))
			Expect(f.Detail).To(ContainSubstring("for 1 of 1 order profiles"))
		})

		It("reports a store only a tag shuts out, for that profile only", func() {
			w := newWorld().hub("HUB-01").stores("BER", "DE", 6).
				edge("h1", "HUB-01", except(network.TypeManaged, "BER-01"), not(network.ContextTag, "bulky")).
				edge("h2", "HUB-01", except(network.TypeManaged, "BER-02"))
			w.s.OrderProfiles = []network.OrderProfile{
				{Tags: []network.TagValue{{ID: "orderType", Value: "bulky"}}, Orders: 1},
				{Tags: []network.TagValue{{ID: "orderType", Value: "express"}}, Orders: 1},
			}

			r := w.check(network.RuleNoSource)

			Expect(subjects(r, network.RuleNoSource)).To(Equal([]string{"BER-02"}))
			Expect(finding(r, network.RuleNoSource, "BER-02").Profiles).To(Equal([]string{"orderType=bulky"}))
			Expect(r.Profiles).To(HaveLen(2))
		})

		It("does not count an offline source", func() {
			w := newWorld().hub("HUB-01").facility("HUB-02", network.TypeManaged, "OFFLINE", "DE").stores("BER", "DE", 6).
				edge("h1", "HUB-01", except(network.TypeManaged, "BER-06")).
				edge("h2", "HUB-02", network.Target{Type: network.TypeManaged})

			Expect(subjects(w.check(network.RuleNoSource), network.RuleNoSource)).To(Equal([]string{"BER-06"}))
		})

		It("does not count a connection that only applies to some items, and says it is there", func() {
			w := newWorld().hub("HUB-01").stores("BER", "DE", 6).
				edge("h1", "HUB-01", except(network.TypeManaged, "BER-06")).
				edge("h2", "HUB-01", network.Target{Type: network.TypeManaged}, in(network.ContextCategory, "dangerous-goods"))

			f := finding(w.check(network.RuleNoSource), network.RuleNoSource, "BER-06")

			Expect(f.Detail).To(ContainSubstring("except connections that depend on an item's category"))
		})
	})

	Describe("peer-coverage", func() {
		It("reports a store that lacks a hub nearly all of its peers have", func() {
			r := germany().check(network.RulePeerCoverage)

			Expect(subjects(r, network.RulePeerCoverage)).To(BeEmpty(), "HUB-01 still reaches BER-11")
		})

		It("reports the store when no hub reaches it but a supplier does", func() {
			w := germany()
			w.s.Edges[0].Target = except(network.TypeManaged, "BER-11")

			r := w.check(network.RulePeerCoverage, network.RuleNoSource)

			Expect(subjects(r, network.RulePeerCoverage)).To(Equal([]string{"BER-11"}))
			Expect(subjects(r, network.RuleNoSource)).To(BeEmpty())
			f := finding(r, network.RulePeerCoverage, "BER-11")
			Expect(f.Detail).To(Equal("no hub reaches it, though one reaches 90% of its peers in DE"))
			Expect(f.Evidence).To(HaveKeyWithValue("kind", "hub"))
		})

		It("compares no store with a peer group below the minimum size, but still reports its coverage", func() {
			w := newWorld().hub("HUB-01").stores("VIE", "AT", 6).
				edge("h1", "HUB-01", except(network.TypeManaged, "VIE-06"))

			r := w.check(network.RulePeerCoverage)

			Expect(subjects(r, network.RulePeerCoverage)).To(BeEmpty())
			Expect(r.Coverage).To(ContainElement(network.Coverage{
				Kind: network.KindHub, Group: "AT", Have: 5, Size: 6, Profile: "(no tags)"}))
		})

		It("reports a gap most of the peer group shares as coverage, not as outliers", func() {
			w := germany()
			// HUB-01 now reaches only BER-01..05 and HUB-02, which reaches nobody any
			// more and so is a store: half of DE lacks a hub, and no outlier rule can
			// call half an outlier.
			w.s.Edges[0].Target = except(network.TypeManaged, "BER-06", "BER-07", "BER-08", "BER-09", "BER-10", "BER-11")
			w.s.Edges[1].Target = network.Target{Type: network.TypeManaged, FacilityRef: id("GONE-01")}

			r := w.check(network.RulePeerCoverage)

			Expect(subjects(r, network.RulePeerCoverage)).To(BeEmpty())
			Expect(r.Coverage).To(ContainElement(network.Coverage{
				Kind: network.KindHub, Group: "DE", Have: 6, Size: 12, Profile: "(no tags)"}))
		})
	})

	Describe("dangling-ref", func() {
		It("reports every reference to something the network does not have, once per reference", func() {
			w := germany().
				edge("d1", "HUB-01", network.Target{Type: network.TypeManaged, FacilityRef: id("GONE-01")}).
				edge("d2", "HUB-01", network.Target{Type: network.TypeManaged, ExcludedGroups: []string{"no-such-group"}},
					in(network.ContextFacility, id("BER-01"), id("GONE-01")),
					in(network.ContextTag, "expres"))
			w.s.Tags = []network.Tag{{ID: "orderType", AllowedValues: []string{"express"}}}

			r := w.check(network.RuleDanglingRef)

			Expect(subjects(r, network.RuleDanglingRef)).To(ConsistOf("expres", id("GONE-01"), id("GONE-01"), "no-such-group"))
			f := finding(r, network.RuleDanglingRef, "expres")
			Expect(f.Detail).To(Equal("the TAG_REFERENCE context value expres names nothing in the network, in 1 connection"))
			Expect(f.Facilities).To(Equal([]string{"HUB-01"}))
		})

		It("does not judge tag values when the snapshot has no tags to judge them by", func() {
			w := germany().edge("x", "HUB-01", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "anything"))

			Expect(subjects(w.check(network.RuleDanglingRef), network.RuleDanglingRef)).To(BeEmpty())
		})
	})

	Describe("offline-ref", func() {
		It("reports an offline facility connections still lead to, once however many do", func() {
			w := germany().facility("OLD-01", network.TypeManaged, "OFFLINE", "DE").
				edge("o1", "HUB-01", network.Target{Type: network.TypeManaged, FacilityRef: id("OLD-01")}).
				edge("o2", "HUB-02", network.Target{Type: network.TypeManaged}, in(network.ContextFacility, id("OLD-01"), id("BER-01"))).
				edge("o3", "HUB-02", network.Target{Type: network.TypeManaged}, not(network.ContextFacility, id("OLD-01")))

			r := w.check(network.RuleOfflineRef)

			Expect(subjects(r, network.RuleOfflineRef)).To(Equal([]string{"OLD-01"}))
			f := finding(r, network.RuleOfflineRef, "OLD-01")
			Expect(f.Detail).To(Equal("it is offline, and 2 connections still lead to it"))
			Expect(f.Evidence).To(HaveKeyWithValue("connections", ConsistOf("o1", "o2")))
		})
	})

	Describe("duplicate-edge", func() {
		It("reports connections of one source that differ only in the order of their context", func() {
			w := germany().
				edge("a", "HUB-01", network.Target{Type: network.TypeManaged},
					in(network.ContextTag, "express", "sameday"), not(network.ContextFacility, id("BER-01"))).
				edge("b", "HUB-01", network.Target{Type: network.TypeManaged},
					not(network.ContextFacility, id("BER-01")), in(network.ContextTag, "sameday", "express")).
				edge("c", "HUB-02", network.Target{Type: network.TypeManaged},
					in(network.ContextTag, "express", "sameday"), not(network.ContextFacility, id("BER-01")))

			r := w.check(network.RuleDuplicateEdge)

			Expect(subjects(r, network.RuleDuplicateEdge)).To(Equal([]string{"HUB-01"}))
			Expect(finding(r, network.RuleDuplicateEdge, "HUB-01").Evidence).To(HaveKeyWithValue("connections", []string{"a", "b"}))
		})

		It("says so when the duplicates differ in their transit time, so the user keeps the right one", func() {
			w := germany().
				edge("a", "HUB-01", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "express")).
				edge("b", "HUB-01", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "express"))
			w.s.Edges[len(w.s.Edges)-2].Transit = &network.Transit{Min: 1, Max: 2}
			w.s.Edges[len(w.s.Edges)-1].Transit = &network.Transit{Min: 3, Max: 4}

			f := finding(w.check(network.RuleDuplicateEdge), network.RuleDuplicateEdge, "HUB-01")

			Expect(f.Detail).To(ContainSubstring("transit"))
			Expect(f.Evidence).To(HaveKey("transit"))
			Expect(f.Next).NotTo(HavePrefix("Keep one."))
		})
	})

	It("runs only the rules it is asked to", func() {
		w := germany().facility("OLD-01", network.TypeManaged, "OFFLINE", "DE").
			edge("o1", "HUB-01", network.Target{Type: network.TypeManaged, FacilityRef: id("OLD-01")}).
			edge("d1", "HUB-01", network.Target{Type: network.TypeManaged, FacilityRef: id("GONE-01")})

		r := w.check(network.RuleOfflineRef)

		Expect(r.Findings).To(HaveLen(1))
		Expect(r.Findings[0].Rule).To(Equal(network.RuleOfflineRef))
	})

	It("reports a clean network as clean", func() {
		Expect(germany().check().Findings).To(BeEmpty())
	})
})
