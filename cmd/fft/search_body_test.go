package main

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("a search --file body", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	// emptyTenant answers every search with no results, under the envelope field
	// the searched entity uses.
	emptyTenant := func(items string) *tenant {
		return c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, envelope(items, nil, false, "", nil))
		})
	}

	DescribeTable("goes over the wire as written",
		func(noun, items, file string, flags []string, want string) {
			api := emptyTenant(items)

			args := append([]string{noun, "search", "--file", tempFile(file)}, flags...)
			Expect(c.run(args...)).To(Equal(exitcode.OK), c.errOut())

			Expect(api.only().Method).To(Equal(http.MethodPost))
			Expect(string(api.only().Body)).To(Equal(want))
		},
		// Regression: the body was re-encoded through the generated NumberFilter, whose
		// fields are float32, and these went out as 16777216 and 1234567.9 — an
		// exclusion that excluded something else, a price nobody asked for.
		Entry("an integer past float32's precision", "stock", "stocks",
			`{"query":{"value":{"notEq":16777217}}}`, nil,
			`{"query":{"value":{"notEq":16777217}}}`),
		Entry("a decimal past float32's precision", "listing", "listings",
			`{"query":{"price":{"eq":1234567.89}}}`, nil,
			`{"query":{"price":{"eq":1234567.89}}}`),
		Entry("an integer past float64's precision", "stock", "stocks",
			`{"query":{"value":{"in":[9007199254740993,1e3]}}}`, nil,
			`{"query":{"value":{"in":[9007199254740993,1e3]}}}`),
		Entry("the sort, and the size the file chose", "facility", "facilities",
			`{"query":{"name":{"like":"Berlin.*"}}, "size": 10, "sort":[{"name":"ASC"}]}`, nil,
			`{"query":{"name":{"like":"Berlin.*"}},"size":10,"sort":[{"name":"ASC"}]}`),
		Entry("with the flags' size and total in place of the file's", "stock", "stocks",
			`{"query":{"value":{"gte":0.1}},"size":10,"options":{"withTotal":false}}`, []string{"--size", "5", "--total"},
			`{"query":{"value":{"gte":0.1}},"size":5,"options":{"withTotal":true}}`),
		Entry("with an empty query when the file has none", "facility", "facilities",
			`{"size":3}`, nil,
			`{"query":{},"size":3}`),
		Entry("with a custom attribute the schema cannot type, null and all", "stock", "stocks",
			`{"query":{"customAttributes":{"lot":null}}}`, nil,
			`{"query":{"customAttributes":{"lot":null}}}`),
	)

	It("sends the query as written on every page --all fetches", func() {
		var pages int
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			pages++
			if pages == 1 {
				writeJSON(w, http.StatusOK, envelope("stocks", []string{fixture("stock.json")}, true, "c1", nil))
				return
			}
			writeJSON(w, http.StatusOK, envelope("stocks", []string{fixture("stock_empty.json")}, false, "c2", nil))
		})

		file := tempFile(`{"query":{"value":{"notEq":16777217}}}`)
		Expect(c.run("stock", "search", "--file", file, "--all", "--max-items", "5")).To(Equal(exitcode.OK), c.errOut())

		calls := api.recorded()
		Expect(calls).To(HaveLen(2))
		Expect(string(calls[0].Body)).To(Equal(`{"query":{"value":{"notEq":16777217}},"size":100}`))
		Expect(string(calls[1].Body)).To(Equal(`{"query":{"value":{"notEq":16777217}},"after":"c1","size":100}`))
	})

	It("stops at --max-items and says so", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, envelope("stocks",
				[]string{fixture("stock.json"), fixture("stock_empty.json")}, true, "c1", nil))
		})

		file := tempFile(`{"query":{"value":{"gt":1}}}`)
		Expect(c.run("stock", "search", "--file", file, "--all", "--max-items", "1")).To(Equal(exitcode.OK), c.errOut())

		Expect(c.errOut()).To(ContainSubstring("stopped after 1 items"))
	})

	// Now that the bytes go as written, a body encoding/json would have read one way
	// can no longer be trusted to be read that way by the API.
	DescribeTable("is refused before anything is sent",
		func(noun, file, want string) {
			api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

			Expect(c.run(noun, "search", "--file", tempFile(file))).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring(want))
			Expect(api.recorded()).To(BeEmpty())
		},
		Entry("when a field is in another case", "stock",
			`{"query":{"VALUE":{"eq":1}}}`, `query.VALUE: the field is spelled "value"`),
		Entry("when a filter operator is in another case", "stock",
			`{"query":{"and":[{"value":{"EQ":1}}]}}`, `query.and[0].value.EQ: the field is spelled "eq"`),
		Entry("when a top-level field is in another case", "facility",
			`{"query":{},"SIZE":3}`, `SIZE: the field is spelled "size"`),
		Entry("when a sort field is in another case", "facility",
			`{"sort":[{"NAME":"ASC"}]}`, `sort[0].NAME: the field is spelled "name"`),
		Entry("when a field is given twice", "stock",
			`{"query":{"value":{"eq":1},"value":{"eq":2}}}`, `query.value is given twice`),
		Entry("when a field is given twice in two cases", "stock",
			`{"query":{"value":{"eq":1},"Value":{"eq":2}}}`, `query.Value: the field is spelled "value"`),
		Entry("when a custom attribute is given twice", "stock",
			`{"query":{"customAttributes":{"lot":"a","lot":"b"}}}`, `query.customAttributes.lot is given twice`),
		Entry("when a filter is null", "listing",
			`{"query":{"price":null}}`, `query.price is null`),
		Entry("when the query is null", "listing",
			`{"query":null}`, `query is null`),
		Entry("when the file holds two documents", "facility",
			`{"query":{}} {"query":{"status":{"eq":"ONLINE"}}}`, `does not contain valid JSON`),
		Entry("when a field is unknown", "facility",
			`{"query":{"statuz":{"eq":"ONLINE"}}}`, `statuz`),
	)
})
