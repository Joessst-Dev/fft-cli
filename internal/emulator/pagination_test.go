package emulator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cursor pagination", func() {
	docs := func(n int) []entityDoc {
		out := make([]entityDoc, n)
		for i := range out {
			out[i] = entityDoc{"id": string(rune('a' + i))}
		}
		return out
	}

	It("walks the whole collection, advancing the cursor each page", func() {
		all := docs(5)

		// Page 1.
		p1, err := paginateCursor(all, "", 2, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(p1.items).To(HaveLen(2))
		Expect(p1.hasNext).To(BeTrue())
		Expect(p1.endCursor).NotTo(BeEmpty())

		// Page 2 starts after page 1's cursor.
		p2, err := paginateCursor(all, p1.endCursor, 2, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(idOf(p2.items[0])).To(Equal("c"))
		Expect(p2.endCursor).NotTo(Equal(p1.endCursor), "the cursor must advance or SearchAll loops")

		// Page 3 is the last: no next page, empty cursor.
		p3, err := paginateCursor(all, p2.endCursor, 2, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(p3.items).To(HaveLen(1))
		Expect(p3.hasNext).To(BeFalse())
		Expect(p3.endCursor).To(BeEmpty(), "a non-empty cursor on the last page is an infinite loop")
	})

	It("omits the total unless it was asked for", func() {
		all := docs(3)

		without, _ := paginateCursor(all, "", 10, false)
		Expect(without.total).To(BeNil())

		with, _ := paginateCursor(all, "", 10, true)
		Expect(with.total).NotTo(BeNil())
		Expect(*with.total).To(Equal(3))
	})

	It("returns an empty last page rather than reading past the end", func() {
		all := docs(2)
		page, err := paginateCursor(all, encodeCursor(2), 10, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(page.items).To(BeEmpty())
		Expect(page.hasNext).To(BeFalse())
	})

	It("rejects a corrupt cursor", func() {
		_, err := paginateCursor(docs(1), "not-base64!!", 10, false)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("startAfterId pagination", func() {
	docs := func(ids ...string) []entityDoc {
		out := make([]entityDoc, len(ids))
		for i, id := range ids {
			out[i] = entityDoc{"id": id}
		}
		return out
	}

	It("returns the first page from the top when no cursor is given", func() {
		page := paginateAfterID(docs("a", "b", "c"), "", 2)
		Expect(page).To(HaveLen(2))
		Expect(idOf(page[0])).To(Equal("a"))
	})

	It("returns the page after the given id", func() {
		page := paginateAfterID(docs("a", "b", "c", "d"), "b", 2)
		Expect(idOf(page[0])).To(Equal("c"))
		Expect(idOf(page[1])).To(Equal("d"))
	})

	It("returns a short final page", func() {
		page := paginateAfterID(docs("a", "b", "c"), "b", 10)
		Expect(page).To(HaveLen(1))
		Expect(idOf(page[0])).To(Equal("c"))
	})
})

var _ = Describe("clampSize", func() {
	It("applies the fallback for an absent size", func() {
		Expect(clampSize(0, defaultListSize)).To(Equal(defaultListSize))
	})

	It("pulls an out-of-range size back into bounds", func() {
		Expect(clampSize(9999, defaultSearchSize)).To(Equal(maxSize))
		Expect(clampSize(-4, defaultSearchSize)).To(Equal(minSize))
	})
})
