package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

func stockPage(stocks ...string) string {
	return envelope("stocks", stocks, false, "", nil)
}

var _ = Describe("fft stock list", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	When("the tenant has stocks", func() {
		var api *tenant

		BeforeEach(func() {
			api = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, stockPage(fixture("stock.json"), fixture("stock_empty.json")))
			})
		})

		It("renders value, reserved and available, so nobody reads one for another", func() {
			Expect(c.run("stock", "list")).To(Equal(exitcode.OK))

			Expect(c.out()).To(Equal(strings.Join([]string{
				"ID                                     TENANT ARTICLE ID   FACILITY                               LOCATION     VALUE   RESERVED   AVAILABLE   VERSION",
				"019c41f1-8f14-7000-9575-25a1b5c8b3a1   4711                8f14e45f-ceea-467a-9575-25a1b5c8b3a1   shelf-a-12   12      2          10          41",
				"019c41f1-8f14-7000-9575-25a1b5c8b3a2   4712                b1946ac9-2492-4ba0-9a6f-2f4f2b2a1f77   -            0       0          0           7",
				"",
			}, "\n")))
		})

		It("searches on the cursor API, not the legacy GET list", func() {
			Expect(c.run("stock", "list")).To(Equal(exitcode.OK))

			// The legacy GET /api/stocks has a different page size (25/100) and cannot
			// express these filters. It is deliberately not what `list` is built on.
			Expect(api.only().Method).To(Equal(http.MethodPost))
			Expect(api.only().Path).To(Equal("/api/stocks/search"))
			Expect(api.only().json()).To(HaveKeyWithValue("query", BeEmpty()))
		})

		It("emits the API's own JSON on stdout and leaves stderr clean with -o json", func() {
			Expect(c.run("stock", "list", "-o", "json")).To(Equal(exitcode.OK))

			var stocks []map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &stocks)).To(Succeed())
			Expect(stocks).To(HaveLen(2))

			// serializedProperties never reaches the table, and must still reach jq.
			Expect(stocks[0]).To(HaveKey("serializedProperties"))
			Expect(c.errOut()).To(BeEmpty())
		})
	})

	DescribeTable("filtering by facility with the spelling that value actually has",
		func(facility, wantKey string) {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, stockPage(fixture("stock.json")))
			})

			Expect(c.run("stock", "list", "--facility", facility)).To(Equal(exitcode.OK))

			// Filtering facilityRef by "BER-01" is *accepted* by the API and quietly
			// matches nothing — a 200 that reads as "there is no stock here" rather than
			// as "you asked the wrong question". The two spellings are not
			// interchangeable and the shape of the value is the only signal.
			Expect(api.only().json()).To(HaveKeyWithValue("query",
				HaveKeyWithValue(wantKey, HaveKeyWithValue("eq", facility))))
		},
		Entry("a tenantFacilityId", "BER-01", "tenantFacilityId"),
		Entry("a platform UUID", "8f14e45f-ceea-467a-9575-25a1b5c8b3a1", "facilityRef"),
	)

	It("reports a total the API sent, and none when it did not", func() {
		total := 2133
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
			var payload map[string]any
			Expect(json.Unmarshal(body, &payload)).To(Succeed())

			// Absent is not zero: the API omits `total` entirely unless asked.
			if _, asked := payload["options"]; asked {
				writeJSON(w, http.StatusOK, envelope("stocks", []string{fixture("stock.json")}, false, "", &total))
				return
			}
			writeJSON(w, http.StatusOK, stockPage(fixture("stock.json")))
		})

		Expect(c.run("stock", "list", "--total")).To(Equal(exitcode.OK))
		Expect(c.errOut()).To(ContainSubstring("Total: 2133"))

		Expect(c.run("stock", "list")).To(Equal(exitcode.OK))
		Expect(c.errOut()).NotTo(ContainSubstring("Total"))
	})
})

var _ = Describe("fft stock create", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("prints an example body without a project, a credential or a request", func() {
		Expect(c.run("stock", "create", "--example")).To(Equal(exitcode.OK))

		var body map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &body)).To(Succeed())
		Expect(body).To(HaveKeyWithValue("tenantArticleId", "4711"))
		Expect(body).To(HaveKey("value"))
		Expect(body).To(HaveKey("facility"))
		Expect(c.errOut()).To(BeEmpty())
	})

	It("builds the body from the flags, picking the facility spelling that value needs", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, fixture("stock.json"))
		})

		Expect(c.run("stock", "create", "--tenant-article-id", "4711", "--facility", "BER-01", "--value", "12")).
			To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/stocks"))

		body := api.only().json()
		Expect(body).To(HaveKeyWithValue("tenantArticleId", "4711"))
		Expect(body).To(HaveKeyWithValue("value", BeNumerically("==", 12)))

		// The object form, not the two deprecated scalars — and inside it, the key the
		// shape of "BER-01" calls for.
		Expect(body).To(HaveKeyWithValue("facility", HaveKeyWithValue("tenantFacilityId", "BER-01")))
	})

	It("sends --value 0, because an empty shelf is a fact worth writing", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, fixture("stock_empty.json"))
		})

		Expect(c.run("stock", "create", "--tenant-article-id", "4712", "--facility", "BER-01", "--value", "0")).
			To(Equal(exitcode.OK))

		Expect(api.only().json()).To(HaveKeyWithValue("value", BeNumerically("==", 0)))
	})

	It("names the created stock by the id the API gave it, not by what it was sent", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, fixture("stock.json"))
		})

		Expect(c.run("stock", "create", "--tenant-article-id", "4711", "--facility", "BER-01", "--value", "12")).
			To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("Created stock 019c41f1-8f14-7000-9575-25a1b5c8b3a1."))
	})

	It("never sends the create twice, whatever the API answers", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusInternalServerError, `[{"summary":"the server is having a moment"}]`)
		})

		Expect(c.run("stock", "create", "--tenant-article-id", "4711", "--facility", "BER-01", "--value", "12")).
			To(Equal(exitcode.Unavailable))

		// A 500 on a POST does not mean the stock was not created — it means fft was
		// not told. Retrying would risk a second one.
		Expect(api.calls).To(HaveLen(1))
	})

	Describe("the facility selector, which the schema does not require and the server does", func() {
		It("refuses a file that names no facility, before a byte goes over the wire", func() {
			api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

			file := tempFile(`{"tenantArticleId":"4711","value":12}`)

			Expect(c.run("stock", "create", "--file", file)).To(Equal(exitcode.Usage))

			// StockForCreation marks none of the three as required, so this body passes
			// schema validation and is rejected by the server with an error that does not
			// name the field.
			Expect(c.errOut()).To(ContainSubstring("does not say which facility"))
			Expect(c.errOut()).To(ContainSubstring(`"facility"`))
			Expect(api.calls).To(BeEmpty())
		})

		It("refuses a file that names it twice, rather than letting the server pick one", func() {
			api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

			file := tempFile(`{"tenantArticleId":"4711","value":12,
				"facility":{"tenantFacilityId":"BER-01"},"facilityRef":"8f14e45f-ceea-467a-9575-25a1b5c8b3a1"}`)

			Expect(c.run("stock", "create", "--file", file)).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring("names the facility 2 times"))
			Expect(api.calls).To(BeEmpty())
		})

		DescribeTable("accepting any one of the three, including the deprecated two",
			func(body string) {
				api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
					writeJSON(w, http.StatusCreated, fixture("stock.json"))
				})

				Expect(c.run("stock", "create", "--file", tempFile(body))).To(Equal(exitcode.OK))
				Expect(api.calls).To(HaveLen(1))
			},
			Entry("facility", `{"tenantArticleId":"4711","value":1,"facility":{"tenantFacilityId":"BER-01"}}`),
			Entry("facilityRef", `{"tenantArticleId":"4711","value":1,"facilityRef":"8f14e45f-ceea-467a-9575-25a1b5c8b3a1"}`),
			Entry("tenantFacilityId", `{"tenantArticleId":"4711","value":1,"tenantFacilityId":"BER-01"}`),
		)
	})

	It("names every missing flag at once, rather than one per run", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("stock", "create", "--tenant-article-id", "4711")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--facility, --value are required"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft stock update", func() {
	// Regression: `fft stock get A -o json > s.json` followed by
	// `fft stock update B --file s.json` used to PUT a body whose id was A to the
	// stock B — a payload contradicting the entity it addresses, and one keystroke
	// from a write that lands somewhere the user never looked.
	It("refuses a file that describes a different stock from the one addressed", func() {
		c := newCLI()
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		file := tempFile(`{"id":"stock-A","tenantArticleId":"4711","value":20}`)

		Expect(c.run("stock", "update", "stock-B", "--file", file)).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("describes stock stock-A, but you asked to update stock-B"))
		Expect(api.calls).To(BeEmpty())
	})

	It("reads the stock for its version and sends that version back", func() {
		c := newCLI()
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("stock.json"))
		})

		file := tempFile(`{"tenantArticleId":"4711","value":20}`)

		Expect(c.run("stock", "update", "019c41f1-8f14-7000-9575-25a1b5c8b3a1", "--file", file)).
			To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[0].Method).To(Equal(http.MethodGet))

		put := api.calls[1]
		Expect(put.Method).To(Equal(http.MethodPut))
		Expect(put.Path).To(Equal("/api/stocks/019c41f1-8f14-7000-9575-25a1b5c8b3a1"))
		Expect(put.json()).To(HaveKeyWithValue("version", BeNumerically("==", 41)))
		Expect(put.json()).To(HaveKeyWithValue("value", BeNumerically("==", 20)))
	})

	It("skips the read when --if-version says what the version is", func() {
		c := newCLI()
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("stock.json"))
		})

		file := tempFile(`{"tenantArticleId":"4711","value":20}`)

		Expect(c.run("stock", "update", "019c41f1", "--file", file, "--if-version", "9")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPut))
		Expect(api.only().json()).To(HaveKeyWithValue("version", BeNumerically("==", 9)))
	})
})

var _ = Describe("fft stock delete", func() {
	It("refuses on a non-interactive terminal without --yes", func() {
		c := newCLI()
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("stock", "delete", "019c41f1")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--yes"))
		Expect(api.calls).To(BeEmpty())
	})

	It("deletes when the user says yes at the prompt", func() {
		c := newCLI()
		c.answer("y")

		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.WriteHeader(http.StatusNoContent)
		})

		Expect(c.run("stock", "delete", "019c41f1")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodDelete))
		Expect(api.only().Path).To(Equal("/api/stocks/019c41f1"))
		Expect(c.out()).To(BeEmpty())
	})

	It("deletes nothing when the user says no", func() {
		c := newCLI()
		c.answer("n")

		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("stock", "delete", "019c41f1")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("Aborted"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft stock actions", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("posts to the collection, with no stock id anywhere in the path", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `{"name":"DELETE_BY_LOCATIONS","result":{"deleted":7}}`)
		})

		file := tempFile(`{"action":{"name":"DELETE_BY_LOCATIONS","facilityRef":"f-1","locationRefs":["shelf-a-12"]}}`)

		Expect(c.run("stock", "actions", "--file", file, "--yes")).To(Equal(exitcode.OK))

		// This is collection-level, unlike a pickjob's action. There is no {id} in the
		// path, and modelling it as a per-entity action would be wrong.
		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/stocks/actions"))

		// The file's bytes are what is sent: an action body has fields fft has no model
		// for, and re-encoding a decoded form would drop them.
		Expect(api.only().json()).To(HaveKeyWithValue("action",
			HaveKeyWithValue("locationRefs", ConsistOf("shelf-a-12"))))
	})

	It("refuses on a non-interactive terminal without --yes: four of the five actions delete things", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		file := tempFile(`{"action":{"name":"DELETE_BY_IDS","stockIds":["a","b"]}}`)

		Expect(c.run("stock", "actions", "--file", file)).To(Equal(exitcode.Usage))

		Expect(api.calls).To(BeEmpty())
	})

	DescribeTable("refusing a body the API would answer with an opaque 400",
		func(body, want string) {
			api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

			Expect(c.run("stock", "actions", "--file", tempFile(body), "--yes")).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring(want))
			Expect(api.calls).To(BeEmpty())
		},
		Entry("no action at all", `{}`, `has no "action"`),
		Entry("an unknown action", `{"action":{"name":"DROP_EVERYTHING"}}`, `unknown action "DROP_EVERYTHING"`),
		Entry("an action with no name", `{"action":{"stockIds":["a"]}}`, `has no "name"`),
		// The API replaced the plural array with a single action; a user reaching for
		// the old shape should be told, not have fft send something deprecated.
		Entry("the deprecated actions array", `{"actions":[{"name":"DELETE_BY_IDS"}]}`, `deprecated "actions" array`),
	)
})

var _ = Describe("fft stock summary", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	const summaries = `{
		"total": 2,
		"stockSummaries": [
			{"article":{"tenantArticleId":"4711","title":"Adidas Superstar"},
			 "details":{"stockOnHand":12,"reserved":2,"availableOnStock":10,"availableForPicking":10,"safetyStock":0},
			 "includedFacilityRefs":["f-1","f-2"]},
			{"article":{"tenantArticleId":"4712","title":"Adidas Gazelle"},
			 "details":{"stockOnHand":0,"reserved":0,"availableOnStock":0,"availableForPicking":0,"safetyStock":0},
			 "includedFacilityRefs":["f-1"]}
		]
	}`

	It("adds the stock up per article and reports the total on stderr", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, summaries)
		})

		Expect(c.run("stock", "summary")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodGet))
		Expect(api.only().Path).To(Equal("/api/stocks/summaries"))

		Expect(c.out()).To(Equal(strings.Join([]string{
			"TENANT ARTICLE ID   TITLE              ON HAND   RESERVED   AVAILABLE   PICKABLE   SAFETY   FACILITIES",
			"4711                Adidas Superstar   12        2          10          10         0        2",
			"4712                Adidas Gazelle     0         0          0           0          0        1",
			"",
		}, "\n")))

		// A total is metadata, not data.
		Expect(c.errOut()).To(ContainSubstring("Total: 2"))
	})

	It("emits the array of summaries on stdout, not the envelope", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, summaries)
		})

		Expect(c.run("stock", "summary", "-o", "json")).To(Equal(exitcode.OK))

		// The same array shape every other list command emits, so a script does not
		// have to reach through a wrapper that only exists on this one endpoint.
		var got []map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &got)).To(Succeed())
		Expect(got).To(HaveLen(2))
		Expect(got[0]).To(HaveKey("details"))
	})

	// Regression (live tenant, 2026-07-12): summaries for facility 0090000020
	// answered 760 by platform UUID and **0 by URN** — not a 400, a cheerful empty
	// 200. facilityRefs is the one facility parameter fft uses that does not resolve
	// the URN form, so sending one made `--facility <tenantFacilityId>` report that
	// a busy store held no stock at all.
	It("resolves a tenantFacilityId to its platform id, which is the only form facilityRefs accepts", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if strings.HasPrefix(r.URL.Path, "/api/facilities/") {
				writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
				return
			}
			writeJSON(w, http.StatusOK, summaries)
		})

		Expect(c.run("stock", "summary", "--facility", "BER-01")).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))

		// One GET to turn the tenantFacilityId into the id the parameter needs...
		Expect(api.calls[0].Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01"))

		// ...and the summaries are then asked for by that id, never by the URN.
		Expect(api.calls[1].Query["facilityRefs"]).To(ConsistOf("8f14e45f-ceea-467a-9575-25a1b5c8b3a1"))
	})

	It("does not pay for a lookup when it was given the platform id already", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, summaries)
		})

		Expect(c.run("stock", "summary", "--facility", "8f14e45f-ceea-467a-9575-25a1b5c8b3a1")).
			To(Equal(exitcode.OK))

		Expect(api.only().Path).To(Equal("/api/stocks/summaries"))
		Expect(api.only().Query["facilityRefs"]).To(ConsistOf("8f14e45f-ceea-467a-9575-25a1b5c8b3a1"))
	})

	DescribeTable("refusing a page size this endpoint would reject",
		func(size string) {
			api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

			Expect(c.run("stock", "summary", "--size", size)).To(Equal(exitcode.Usage))

			// This endpoint's limit is 1–100, NOT the search API's 1–250. The two are
			// deliberately not unified.
			Expect(c.errOut()).To(ContainSubstring("between 1 and 100"))
			Expect(api.calls).To(BeEmpty())
		},
		Entry("zero", "0"),
		Entry("negative", "-1"),
		Entry("the search API's maximum, which is not this one's", "250"),
	)

	It("says when it is showing only part of the answer", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `{"total":383484,"stockSummaries":[
				{"article":{"tenantArticleId":"4711","title":"x"},
				 "details":{"stockOnHand":1,"reserved":0,"availableOnStock":1,"availableForPicking":1,"safetyStock":0},
				 "includedFacilityRefs":["f-1"]}
			]}`)
		})

		Expect(c.run("stock", "summary")).To(Equal(exitcode.OK))

		// This endpoint has no --all — it pages on its own startAfterId cursor, not the
		// search API's. The least it can do is not leave a total of 383,484 sitting
		// above a single row without a word.
		Expect(c.errOut()).To(ContainSubstring("Showing 1 of 383484 articles"))
	})

	It("accepts a size the search API would also accept, and sends it", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, summaries)
		})

		Expect(c.run("stock", "summary", "--size", "100")).To(Equal(exitcode.OK))
		Expect(api.only().Query.Get("size")).To(Equal("100"))
	})
})

var _ = Describe("fft stock upsert", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	// upsertAnswer echoes the stocks it was sent back as results, which is what the
	// API does.
	upsertAnswer := func(body []byte) string {
		var payload struct {
			Stocks []struct {
				ID              string `json:"id"`
				TenantArticleID string `json:"tenantArticleId"`
			} `json:"stocks"`
		}
		Expect(json.Unmarshal(body, &payload)).To(Succeed())

		results := make([]string, 0, len(payload.Stocks))
		for _, s := range payload.Stocks {
			status := "CREATED"
			if s.ID != "" {
				status = "UPDATED"
			}
			results = append(results, fmt.Sprintf(
				`{"status":%q,"stock":{"id":%q,"tenantArticleId":%q,"facilityRef":"f-1"}}`,
				status, s.ID, s.TenantArticleID))
		}
		return "[" + strings.Join(results, ",") + "]"
	}

	It("upserts in one request when the file fits, and reports each stock", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
			writeJSON(w, http.StatusOK, upsertAnswer(body))
		})

		file := tempFile(`{"stocks":[
			{"tenantArticleId":"4711","value":12,"facility":{"tenantFacilityId":"BER-01"}},
			{"id":"s-1","value":0}
		]}`)

		Expect(c.run("stock", "upsert", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPut))
		Expect(api.only().Path).To(Equal("/api/stocks"))

		Expect(c.out()).To(ContainSubstring("CREATED"))
		Expect(c.out()).To(ContainSubstring("UPDATED"))
		Expect(c.errOut()).To(ContainSubstring("2 stocks: 1 created, 1 updated"))
	})

	It("splits a file larger than one request, dropping nothing", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
			writeJSON(w, http.StatusOK, upsertAnswer(body))
		})

		const n = maxStockUpsert + 7

		stocks := make([]string, n)
		for i := range stocks {
			stocks[i] = fmt.Sprintf(`{"id":"s-%d","value":%d}`, i, i)
		}
		file := tempFile(fmt.Sprintf(`{"stocks":[%s]}`, strings.Join(stocks, ",")))

		Expect(c.run("stock", "upsert", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))

		// Every chunk is within the limit, and the union of the chunks is exactly the
		// file — no entry sent twice, none left behind.
		var sent []string
		for _, call := range api.calls {
			var payload struct {
				Stocks []struct {
					ID string `json:"id"`
				} `json:"stocks"`
			}
			Expect(json.Unmarshal(call.Body, &payload)).To(Succeed())
			Expect(len(payload.Stocks)).To(BeNumerically("<=", maxStockUpsert))

			for _, s := range payload.Stocks {
				sent = append(sent, s.ID)
			}
		}

		Expect(sent).To(HaveLen(n))
		Expect(sent).To(HaveEach(Not(BeEmpty())))
		Expect(sent).To(ConsistOf(expectedStockIDs(n)))
	})

	It("refuses a stock named twice, which the API answers by rejecting the whole batch", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		file := tempFile(`{"stocks":[{"id":"s-1","value":1},{"id":"s-1","value":2}]}`)

		Expect(c.run("stock", "upsert", "--file", file)).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("appears twice, at entries 1 and 2"))
		Expect(api.calls).To(BeEmpty())
	})

	It("applies the facility rule to a create inside the file, before sending anything", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		file := tempFile(`{"stocks":[{"tenantArticleId":"4711","value":12}]}`)

		Expect(c.run("stock", "upsert", "--file", file)).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("does not say which facility"))
		Expect(api.calls).To(BeEmpty())
	})

	// Regression: the reconciliation used to be a *set* of the names the answer
	// mentioned, keyed on both the id and the tenantArticleId. Two creates of the
	// same article at two locations are legal and share a tenantArticleId — so one
	// answer marked both entries reported, and a stock the API silently dropped left
	// no trace at all. Counting rather than set-membership is what makes "two sent,
	// one answered" produce exactly one FAILED row.
	It("does not let one answer account for two entries that share a tenantArticleId", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK,
				`[{"status":"CREATED","stock":{"id":"new-1","tenantArticleId":"4711","facilityRef":"f-1"}}]`)
		})

		// The same article on two shelves: two creates, both keyed "4711".
		file := tempFile(`{"stocks":[
			{"tenantArticleId":"4711","value":1,"facility":{"tenantFacilityId":"BER-01"},"locationRef":"shelf-a"},
			{"tenantArticleId":"4711","value":2,"facility":{"tenantFacilityId":"BER-01"},"locationRef":"shelf-b"}
		]}`)

		Expect(c.run("stock", "upsert", "--file", file)).To(Equal(exitcode.Partial))

		Expect(c.out()).To(ContainSubstring("FAILED"))
		Expect(c.errOut()).To(ContainSubstring("1 of 2 stocks failed"))
	})

	// Regression: an empty (or unreadable) 2xx body used to mark every entry of a
	// chunk that had ALREADY LANDED as FAILED — and partialError's hint says "re-send
	// only those". For a stock create, re-sending makes a second stock. An outcome
	// fft does not know is UNKNOWN, and UNKNOWN does not exit 8.
	It("calls a landed write whose answer it cannot read UNKNOWN, not FAILED", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			// The write happened. The API just said nothing about it.
			w.WriteHeader(http.StatusOK)
		})

		file := tempFile(`{"stocks":[{"tenantArticleId":"4711","value":1,"facility":{"tenantFacilityId":"BER-01"}}]}`)

		Expect(c.run("stock", "upsert", "--file", file)).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring("UNKNOWN"))
		Expect(c.out()).NotTo(ContainSubstring("FAILED"))

		// And the advice is to go and look, never to re-send.
		Expect(c.errOut()).To(ContainSubstring("created a second time"))
		Expect(c.errOut()).NotTo(ContainSubstring("re-send only those"))
	})

	// Regression: an abort mid-run used to discard the results of the chunks that
	// had already landed, so a token expiring on request 2 of 3 left the user with
	// no record of what was written — and for creates, re-running the file duplicates
	// everything that did.
	It("still prints the stocks that landed when the run is cut short", func() {
		var sent int

		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
			sent++
			if sent == 1 {
				writeJSON(w, http.StatusOK, upsertAnswer(body))
				return
			}
			writeJSON(w, http.StatusUnauthorized, `[{"summary":"the token is not valid"}]`)
		})

		stocks := make([]string, maxStockUpsert+1)
		for i := range stocks {
			stocks[i] = fmt.Sprintf(`{"id":"s-%d","value":%d}`, i, i)
		}
		file := tempFile(fmt.Sprintf(`{"stocks":[%s]}`, strings.Join(stocks, ",")))

		// The exit code is the *cause* — an expired token — not exit 8 for a partial
		// write that the auth failure says nothing about.
		Expect(c.run("stock", "upsert", "--file", file)).To(Equal(exitcode.Auth))

		// But the 500 stocks of the first chunk really were written, and this table is
		// the only record the user will get of which ones.
		Expect(c.errOut()).To(ContainSubstring("The run stopped early"))
		Expect(c.out()).To(ContainSubstring("s-0"))
		Expect(c.out()).To(ContainSubstring("UPDATED"))
	})

	It("reports a stock the API accepted but never answered for as FAILED, and exits 8", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			// The API answers 200 with results for one of the two stocks it was sent.
			// Calling that a success would hide exactly the thing the user needs to know.
			writeJSON(w, http.StatusOK,
				`[{"status":"UPDATED","stock":{"id":"s-1","tenantArticleId":"4711","facilityRef":"f-1"}}]`)
		})

		file := tempFile(`{"stocks":[{"id":"s-1","value":1},{"id":"s-2","value":2}]}`)

		Expect(c.run("stock", "upsert", "--file", file)).To(Equal(exitcode.Partial))

		Expect(c.out()).To(ContainSubstring("s-2"))
		Expect(c.out()).To(ContainSubstring("FAILED"))
		Expect(c.out()).To(ContainSubstring("reported no result"))
		Expect(c.errOut()).To(ContainSubstring("1 of 2 stocks failed"))
	})
})

// expectedStockIDs is the set of ids a split-file spec should find on the wire.
func expectedStockIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("s-%d", i)
	}
	return ids
}
