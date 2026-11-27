package main

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// The API's exact answer to a search it will not run on a very large collection.
const queryTooComplex = `[{"summary":"Query too complex - please simplify your search criteria or reduce the result set size"}]`

// genericSentence is a line only the client's generic hint says, so a spec can tell
// "the generic hint" from "a command's own".
const genericSentence = "filter on an exact match"

var _ = Describe("the API's 'Query too complex' 400 on a search", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	tooComplexTenant := func() *tenant {
		return c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusBadRequest, queryTooComplex)
		})
	}

	// Each tier must say that a smaller page is not the way out, keep the API's
	// words and exit code, and leave stdout to the data (which there is none of).
	// It matches only what every hint shares: the generic, stock, listing, facility
	// and order hints each go on to explain the cost in their own words.
	expectExplained := func(code int) {
		GinkgoHelper()

		Expect(code).To(Equal(exitcode.General))
		Expect(c.errOut()).To(ContainSubstring("Query too complex"))
		Expect(c.errOut()).To(ContainSubstring("will not help:"))
		Expect(c.out()).To(BeEmpty())
	}

	Describe("a curated search", func() {
		It("explains it for `stock search --file`, with the generic hint", func() {
			tooComplexTenant()

			expectExplained(c.run("stock", "search", "--file", tempFile(`{"query":{}}`)))

			Expect(c.errOut()).To(ContainSubstring(genericSentence))
		})

		It("names the narrow filters of a stock list", func() {
			tooComplexTenant()

			expectExplained(c.run("stock", "list", "--sort", "value:desc"))

			Expect(c.errOut()).To(ContainSubstring("--facility <id> --tenant-article-id <id>"))
			Expect(c.errOut()).NotTo(ContainSubstring(genericSentence))
		})

		It("names the narrow filter of a facility list", func() {
			tooComplexTenant()

			expectExplained(c.run("facility", "list"))

			Expect(c.errOut()).To(ContainSubstring("--tenant-facility-id <id>"))
		})

		It("names the narrow filter of a listing list", func() {
			c.listingTenant(func(w http.ResponseWriter, _ []byte) {
				writeJSON(w, http.StatusBadRequest, queryTooComplex)
			})

			expectExplained(c.run("listing", "list", "--facility", "BER-01"))

			Expect(c.errOut()).To(ContainSubstring("--tenant-article-id <id>"))
		})
	})

	Describe("a generated Tier-2 search", func() {
		It("explains it for `picking search-pick-job`", func() {
			api := tooComplexTenant()

			expectExplained(c.run("picking", "search-pick-job", "--file", tempFile(`{"query":{}}`)))

			Expect(api.only().Path).To(Equal("/api/pickjobs/search"))
			Expect(c.errOut()).To(ContainSubstring(genericSentence))
			// Tier 2 has no --size flag: the hint must name the body's "size" too.
			Expect(c.errOut()).To(ContainSubstring(`the body's "size"`))
		})
	})

	Describe("the Tier-3 escape hatch", func() {
		It("explains it for `fft api searchListing`", func() {
			tooComplexTenant()

			expectExplained(c.run("api", "searchListing", "--file", tempFile(`{"query":{}}`)))

			Expect(c.errOut()).To(ContainSubstring(genericSentence))
			Expect(c.errOut()).To(ContainSubstring(`the body's "size"`))
		})
	})

	Describe("an --all walk that fails on its second page", func() {
		It("says how far it got, with the hint, and prints no half list", func() {
			calls := 0
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				calls++
				if calls == 1 {
					writeJSON(w, http.StatusOK, envelope("stocks",
						[]string{`{"id":"s1","tenantArticleId":"A-1","value":3}`}, true, "cursor-1", nil))
					return
				}
				writeJSON(w, http.StatusBadRequest, queryTooComplex)
			})

			expectExplained(c.run("stock", "list", "--all"))

			Expect(calls).To(Equal(2))
			Expect(c.errOut()).To(ContainSubstring("after 1 page (1 item) of an --all walk"))
			Expect(c.errOut()).To(ContainSubstring("--facility <id> --tenant-article-id <id>"))
		})

		It("says so for a --file search too, with the generic hint", func() {
			calls := 0
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				calls++
				if calls == 1 {
					writeJSON(w, http.StatusOK, envelope("stocks",
						[]string{`{"id":"s1","tenantArticleId":"A-1","value":3}`}, true, "cursor-1", nil))
					return
				}
				writeJSON(w, http.StatusBadRequest, queryTooComplex)
			})

			expectExplained(c.run("stock", "search", "--file", tempFile(`{"query":{}}`), "--all"))

			Expect(c.errOut()).To(ContainSubstring("after 1 page (1 item) of an --all walk"))
			Expect(c.errOut()).To(ContainSubstring(genericSentence))
		})
	})

	Describe("any other 400", func() {
		// The same status is what an unknown enum value gets; a hint about narrowing
		// the filter would send the user the wrong way.
		const other = `[{"summary":"status must be one of OPEN, CLOSED"}]`

		BeforeEach(func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusBadRequest, other)
			})
		})

		DescribeTable("passes through with the API's message and no hint",
			func(args ...string) {
				// The body file is made here: a spec's temp dir does not exist yet while
				// the table is being built.
				file := tempFile(`{"query":{}}`)
				for i, a := range args {
					if a == "BODY" {
						args[i] = file
					}
				}
				Expect(c.run(args...)).To(Equal(exitcode.General))

				Expect(c.errOut()).To(ContainSubstring("status must be one of OPEN, CLOSED"))
				Expect(c.errOut()).NotTo(ContainSubstring("will not help:"))
				Expect(c.errOut()).NotTo(ContainSubstring(genericSentence))
			},
			Entry("stock search", "stock", "search", "--file", "BODY"),
			Entry("stock list", "stock", "list"),
			Entry("fft api searchListing", "api", "searchListing", "--file", "BODY"),
			Entry("a generated search", "picking", "search-pick-job", "--file", "BODY"),
		)
	})

	It("does not explain the same message from an operation that is not a search", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusBadRequest, queryTooComplex)
		})

		Expect(c.run("api", "getPickJob", "--param", "pickJobId=abc")).To(Equal(exitcode.General))

		Expect(c.errOut()).To(ContainSubstring("Query too complex"))
		Expect(c.errOut()).NotTo(ContainSubstring("will not help:"))
		Expect(c.errOut()).NotTo(ContainSubstring(genericSentence))
	})

	It("lets the order hint replace the generic one rather than add to it", func() {
		tooComplexTenant()

		expectExplained(c.run("order", "search", "--since", "2026-11-24"))

		Expect(c.errOut()).To(ContainSubstring("fft routing decision-logs"))
		Expect(c.errOut()).NotTo(ContainSubstring(genericSentence))
	})
})
