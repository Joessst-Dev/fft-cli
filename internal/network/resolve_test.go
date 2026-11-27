package network_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/network"
)

var _ = Describe("resolving a connection for an order profile", func() {
	DescribeTable("leads where the router would send the order",
		func(sp spec, p network.Profile, verdict network.Verdict, want []string) {
			n := network.New(snapshot(sp))

			r := n.Reach(&n.Snapshot().Edges[0], p)

			Expect(r.Verdict).To(Equal(verdict))
			if verdict != network.Excluded {
				Expect(labels(n, r.Targets)).To(Equal(want))
			}
		},
		Entry("a wildcard reaches every facility of its type but the source",
			spec{target: wildcard(network.TypeManaged)}, profile(), network.Applies, []string{"BER-01", "BER-02"}),
		Entry("a wildcard SUPPLIER target reaches the suppliers only",
			spec{target: wildcard(network.TypeSupplier)}, profile(), network.Applies, []string{"SUP-01"}),
		Entry("an explicit target reaches that facility only",
			spec{target: to(ber2ID)}, profile(), network.Applies, []string{"BER-02"}),
		Entry("an explicit target may name the facility by its tenant id's URN",
			spec{target: to("urn:fft:facility:tenantFacilityId:BER-01")}, profile(), network.Applies, []string{"BER-01"}),
		Entry("an explicit target that names nothing reaches nothing",
			spec{target: to("urn:fft:facility:tenantFacilityId:GONE-01")}, profile(), network.Applies, []string{}),
		Entry("excludedFacilityRefs removes a facility from a wildcard",
			spec{target: network.Target{Type: network.TypeManaged, ExcludedFacilities: []string{ber1ID}}},
			profile(), network.Applies, []string{"BER-02"}),
		Entry("excludedFacilityGroupRefs removes a group's members",
			spec{target: network.Target{Type: network.TypeManaged, ExcludedGroups: []string{berlinGroup}}},
			profile(), network.Applies, []string{"BER-02"}),
		Entry("FACILITY matches the receiving facility",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextFacility, ber1ID)}},
			profile(), network.Applies, []string{"BER-01"}),
		Entry("FACILITY NOT removes the receiving facilities it lists",
			spec{target: wildcard(network.TypeManaged), context: []entry{not(network.ContextFacility, ber1ID)}},
			profile(), network.Applies, []string{"BER-02"}),
		Entry("FACILITY_GROUP matches the receiving facility's groups",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextFacilityGroup, berlinGroup)}},
			profile(), network.Applies, []string{"BER-01"}),
		Entry("FACILITY_GROUP NOT removes the groups' members",
			spec{target: wildcard(network.TypeManaged), context: []entry{not(network.ContextFacilityGroup, berlinGroup)}},
			profile(), network.Applies, []string{"BER-02"}),
		Entry("a TAG_REFERENCE entry applies when the order carries one of its values",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextTag, "express", "sameday")}},
			profile("sameday"), network.Applies, []string{"BER-01", "BER-02"}),
		Entry("a tag the order lacks fails a positive entry",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextTag, "express")}},
			profile(), network.Excluded, nil),
		Entry("a tag the order lacks passes a NOT entry",
			spec{target: wildcard(network.TypeManaged), context: []entry{not(network.ContextTag, "express")}},
			profile(), network.Applies, []string{"BER-01", "BER-02"}),
		Entry("a NOT entry with several values excludes the order if any one matches",
			spec{target: wildcard(network.TypeManaged), context: []entry{not(network.ContextTag, "express", "bulky")}},
			profile("bulky"), network.Excluded, nil),
		Entry("entries combine with AND",
			spec{target: wildcard(network.TypeManaged), context: []entry{
				in(network.ContextTag, "express"), in(network.ContextFacility, ber1ID, ber2ID), not(network.ContextFacility, ber2ID),
			}},
			profile("express"), network.Applies, []string{"BER-01"}),
		Entry("a CATEGORY entry makes the connection conditional, and keeps its targets",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextCategory, "dangerous-goods")}},
			profile(), network.Conditional, []string{"BER-01", "BER-02"}),
		Entry("a recipient-country entry makes the connection conditional",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextTargetCountry, "DE")}},
			profile(), network.Conditional, []string{"BER-01", "BER-02"}),
		Entry("a FACILITY value that names no facility matches none",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextFacility, "urn:fft:facility:tenantFacilityId:GONE-01")}},
			profile(), network.Applies, []string{}),
	)

	It("leads to no facility when the target is the customer", func() {
		n := network.New(snapshot(spec{target: wildcard(network.TypeCustomer)}))

		r := n.Reach(&n.Snapshot().Edges[0], profile())

		Expect(r.Verdict).To(Equal(network.Applies))
		Expect(r.Targets).To(BeNil())
	})

	It("names the entry that excluded the profile", func() {
		n := network.New(snapshot(spec{target: wildcard(network.TypeManaged), context: []entry{
			in(network.ContextFacility, ber1ID), in(network.ContextTag, "express"),
		}}))

		r := n.Reach(&n.Snapshot().Edges[0], profile("bulky"))

		Expect(r.Verdict).To(Equal(network.Excluded))
		Expect(r.Entry).To(Equal(1))
	})

	It("does not reuse a memoised set across value lists", func() {
		n := network.New(snapshot(
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextFacility, ber1ID)}},
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextFacility, ber2ID)}},
		))
		edges := n.Snapshot().Edges

		Expect(labels(n, n.Reach(&edges[0], profile()).Targets)).To(Equal([]string{"BER-01"}))
		Expect(labels(n, n.Reach(&edges[1], profile()).Targets)).To(Equal([]string{"BER-02"}))
		Expect(labels(n, n.Reach(&edges[0], profile()).Targets)).To(Equal([]string{"BER-01"}))
	})
})

var _ = Describe("explaining a connection for one facility", func() {
	ber := func(n *network.Network, ref string) int {
		i, ok := n.Lookup(ref)
		Expect(ok).To(BeTrue())
		return i
	}

	DescribeTable("names the first thing that rules the facility out",
		func(sp spec, p network.Profile, facility string, applies bool, entry int, why string) {
			n := network.New(snapshot(sp))

			r := n.Explain(&n.Snapshot().Edges[0], ber(n, facility), p)

			Expect(r.Applies).To(Equal(applies))
			Expect(r.Entry).To(Equal(entry))
			Expect(r.Why).To(ContainSubstring(why))
		},
		Entry("a wildcard that passes every entry",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextTag, "express")}},
			profile("express"), ber1ID, true, -1, "every context entry passes"),
		Entry("the source itself",
			spec{target: wildcard(network.TypeManaged)}, profile(), hubID, false, -1, "own source"),
		Entry("another explicit target",
			spec{target: to(ber2ID)}, profile(), ber1ID, false, -1, "targets "+ber2ID+" only"),
		Entry("a target of another type",
			spec{target: wildcard(network.TypeSupplier)}, profile(), ber1ID, false, -1, "targets SUPPLIER facilities"),
		Entry("an excluded group",
			spec{target: network.Target{Type: network.TypeManaged, ExcludedGroups: []string{berlinGroup}}},
			profile(), ber1ID, false, -1, "excludedFacilityGroupRefs lists group "+berlinGroup),
		Entry("a FACILITY entry that does not list it",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextFacility, ber2ID)}},
			profile(), ber1ID, false, 0, "context[0] FACILITY does not list it"),
		Entry("a NOT tag the order carries",
			spec{target: wildcard(network.TypeManaged), context: []entry{
				in(network.ContextFacility, ber1ID), not(network.ContextTag, "express", "bulky"),
			}},
			profile("bulky"), ber1ID, false, 1, "the order carries bulky"),
		Entry("a positive tag the order lacks",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextTag, "a", "b", "c", "d")}},
			profile(), ber1ID, false, 0, "none of a, b, c (and 1 more)"),
		Entry("a CATEGORY entry, which applies only to some orders",
			spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextCategory, "dangerous-goods")}},
			profile(), ber1ID, true, 0, "depends on the order line"),
	)
})
