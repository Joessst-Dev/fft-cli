package emulator

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/api"
)

// knownCollections is the other half of the single-segment census: the /api/{name}
// paths that really are collections of entities, and are therefore right to serve
// from the store. It is a fixture, not production code, and it exists so that the
// two lists together must account for every single-segment path in the spec.
//
// That is the whole point. The upstream spec is versionless and regenerated without
// notice, so a sync can introduce a top-level path nobody has looked at. Defaulting
// it to a collection is not fail-closed: a singleton served that way answers an
// empty list envelope with a 200, which reads as "the API works and there is nothing
// there" — the failure mode that hid GET /api/status for as long as it did. So the
// census below fails the build instead, and someone decides which list it joins.
var knownCollections = []string{
	"articles",
	"audits",
	"brands",
	"cancelationreasons",
	"carriers",
	"categories",
	"customservices",
	"deliverynotes",
	"expiries",
	"externalactions",
	"externalstockchangereasons",
	"facilities",
	"facilitygroups",
	"features",
	"handovercontainers",
	"handoverjobs",
	"inboundprocesses",
	"inboundreceiptjobs",
	"itemreturnjobs",
	"itemreturns",
	"labels",
	"linkedservicejobs",
	"listings",
	"loadunits",
	"loadunittypes",
	"lookuprecords",
	"measurementunits",
	"operativecontainertypes",
	"operativeprocesses",
	"orderrecords",
	"orders",
	"packagingunits",
	"packingcontainertypes",
	"packingsourcecontainers",
	"packjobs",
	"parcelinformation",
	"parcels",
	"permissions",
	"pickjobs",
	"pickruns",
	"pickupservices",
	"processes",
	"purchaseorders",
	"receipts",
	"remoteconfigs",
	"reroutedescriptions",
	"reservations",
	"restowitems",
	"returnnotes",
	"roles",
	"routingplans",
	"safetystocks",
	"scopedcapabilities",
	"servicecontainers",
	"servicejobs",
	"shipments",
	"shippinginformation",
	"signatures", // POST-only PDF render with no GET; wants kindStateless, but singletons
	// requires a SampleResponse and specgen synthesizes none for a binary reply — see
	// classify's "gives every singleton a sample to answer with" spec.
	"stacks",
	"stocks",
	"stowjobs",
	"subscriptions",
	"substitutes",
	"tags",
	"trackinginformation",
	"users",
}

// specSegments is every distinct first segment under /api/ that the spec addresses
// on its own, i.e. the paths classify has to decide about.
func specSegments() map[string]bool {
	segments := map[string]bool{}

	for _, op := range api.Operations() {
		rest, ok := strings.CutPrefix(op.Path, "/api/")
		if !ok {
			continue
		}

		parts := pathSegments(rest)
		if len(parts) != 1 || isParam(parts[0]) {
			continue
		}
		segments[parts[0]] = true
	}
	return segments
}

var _ = Describe("classify", func() {
	Describe("the single-segment census", func() {
		It("classifies every top-level path in the spec exactly once", func() {
			known := map[string]bool{}
			for _, name := range knownCollections {
				known[name] = true
			}

			var unclassified []string
			for name := range specSegments() {
				if !known[name] && !singletons[name] {
					unclassified = append(unclassified, name)
				}
			}

			Expect(unclassified).To(BeEmpty(),
				"the spec has grown a top-level path nobody has classified: add it to "+
					"knownCollections if it is a page of entities, or to singletons in route.go "+
					"if the spec declares one object for it")
		})

		It("has no entry the spec no longer addresses", func() {
			segments := specSegments()

			for _, name := range knownCollections {
				Expect(segments).To(HaveKey(name), "knownCollections names a path the spec dropped")
			}
			for name := range singletons {
				Expect(segments).To(HaveKey(name), "singletons names a path the spec dropped")
			}
		})

		It("puts no path in both lists, and none in one of them twice", func() {
			seen := map[string]bool{}
			for _, name := range knownCollections {
				Expect(singletons).NotTo(HaveKey(name))
				Expect(seen).NotTo(HaveKey(name), "knownCollections lists %q twice", name)
				seen[name] = true
			}
		})
	})

	// The behaviour the census cannot check for itself: it proves every path is
	// classified, not that the classification is right. So every singleton is
	// asserted to actually route statelessly, and a collection to still reach the
	// store.
	It("serves every singleton statelessly, and a collection from the store", func() {
		routed := map[string]bool{}

		for _, op := range api.Operations() {
			coll, _, kind := classify(op)

			if name, ok := strings.CutPrefix(op.Path, "/api/"); ok && singletons[name] {
				Expect(kind).To(Equal(kindStateless), "%s must not be served from the store", op.Path)
				routed[name] = true
				continue
			}

			if op.Path == "/api/facilities" && op.Method == "GET" {
				Expect(kind).To(Equal(kindList))
				Expect(coll).To(Equal("facilities"))
			}
		}

		Expect(routed).To(HaveLen(len(singletons)), "a singleton the spec no longer addresses went unasserted")
	})

	// A singleton is only an improvement if the spec has something to answer with:
	// with no sample, stateless means 204, which trades one wrong shape for another.
	It("gives every singleton a sample to answer with", func() {
		samples := map[string]string{}
		for _, op := range api.Operations() {
			if name, ok := strings.CutPrefix(op.Path, "/api/"); ok && singletons[name] {
				samples[name] = op.SampleResponse
			}
		}

		for name := range singletons {
			Expect(samples[name]).NotTo(BeEmpty(),
				"%q has no SampleResponse, so serving it statelessly would answer 204", name)
		}
	})
})
