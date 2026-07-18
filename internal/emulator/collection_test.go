package emulator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/api"
)

var _ = Describe("collection inference", func() {
	Describe("arrayKey", func() {
		It("finds the array property in a list envelope", func() {
			Expect(arrayKey(`{"facilities":[{"id":"a"}],"total":1}`)).To(Equal("facilities"))
		})

		It("skips pageInfo and finds the entities in a search envelope", func() {
			Expect(arrayKey(`{"pageInfo":{"hasNextPage":false},"facilities":[]}`)).To(Equal("facilities"))
		})

		It("returns nothing when the response carries no array", func() {
			Expect(arrayKey(`{"id":"a","version":1}`)).To(BeEmpty())
			Expect(arrayKey("")).To(BeEmpty())
		})
	})

	// The load-bearing case: the items-key is not the path segment. Inference reads it
	// out of the synthesized response, so a collection whose envelope names its array
	// differently than its path resolves correctly.
	Describe("inferCollections over the real spec", func() {
		var metas map[string]collectionMeta

		BeforeEach(func() {
			metas = inferCollections(api.Operations())
		})

		DescribeTable("resolves a collection's items-key from the spec",
			func(collection, wantKey string) {
				m, ok := metas[collection]
				Expect(ok).To(BeTrue(), "no metadata inferred for %q", collection)
				Expect(m.itemsKey).To(Equal(wantKey))
			},
			Entry("facilities", "facilities", "facilities"),
			Entry("listings", "listings", "listings"),
			Entry("stocks", "stocks", "stocks"),
			Entry("orders", "orders", "orders"),
		)
	})
})
