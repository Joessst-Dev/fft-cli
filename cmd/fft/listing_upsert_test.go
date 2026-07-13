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

// upsertFile builds a bulk payload of n SINGLE_FACILITY entries, article ids a-0…
func upsertFile(n int) string {
	entries := make([]string, n)
	for i := range entries {
		entries[i] = fmt.Sprintf(
			`{"targetingStrategy":"SINGLE_FACILITY","tenantArticleId":"a-%d","title":"Article %d",
			  "facility":{"tenantFacilityId":"BER-01"}}`, i, i)
	}
	return fmt.Sprintf(`{"listings":[%s]}`, strings.Join(entries, ","))
}

// upsertRequest is what fft sent in one chunk, decoded far enough to be checked.
type upsertRequest struct {
	Listings []struct {
		TargetingStrategy string            `json:"targetingStrategy"`
		TenantArticleID   string            `json:"tenantArticleId"`
		Selector          []json.RawMessage `json:"selector"`
	} `json:"listings"`
}

// budget is what the API charges for a chunk: the number of entries times the
// number of facilities the largest of them targets. This is the constraint fft
// must never exceed, so the spec computes it independently of the code under test.
func (r upsertRequest) budget() int {
	most := 0
	for _, l := range r.Listings {
		facilities := 1
		if l.TargetingStrategy == targetMultiSelector {
			facilities = len(l.Selector)
		}
		most = max(most, facilities)
	}
	return len(r.Listings) * most
}

func (r upsertRequest) articles() []string {
	ids := make([]string, 0, len(r.Listings))
	for _, l := range r.Listings {
		ids = append(ids, l.TenantArticleID)
	}
	return ids
}

// decodeUpsert reads every chunk fft sent.
func decodeUpsert(api *tenant) []upsertRequest {
	GinkgoHelper()

	reqs := make([]upsertRequest, 0, len(api.calls))
	for _, call := range api.calls {
		Expect(call.Method).To(Equal(http.MethodPut))
		Expect(call.Path).To(Equal("/api/listings"))

		var req upsertRequest
		Expect(json.Unmarshal(call.Body, &req)).To(Succeed())
		reqs = append(reqs, req)
	}
	return reqs
}

// echoUpsert answers a chunk the way the API does: one success per (listing,
// facility) pair.
func echoUpsert(body []byte) string {
	GinkgoHelper()

	var req upsertRequest
	Expect(json.Unmarshal(body, &req)).To(Succeed())

	successes := make([]string, 0, len(req.Listings))
	for _, l := range req.Listings {
		successes = append(successes, fmt.Sprintf(
			`{"status":"CREATED","result":{"tenantArticleId":%q,"facilityId":"f-1"}}`, l.TenantArticleID))
	}

	return fmt.Sprintf(`{"listings":[%s],"summary":{"created":%d,"updated":0,"unchanged":0}}`,
		strings.Join(successes, ","), len(successes))
}

var _ = Describe("fft listing upsert", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("prints an example showing both targeting strategies, since that is what users get wrong", func() {
		Expect(c.run("listing", "upsert", "--example")).To(Equal(exitcode.OK))

		var body struct {
			Listings []struct {
				TargetingStrategy string `json:"targetingStrategy"`
			} `json:"listings"`
		}
		Expect(json.Unmarshal([]byte(c.out()), &body)).To(Succeed())

		var strategies []string
		for _, l := range body.Listings {
			strategies = append(strategies, l.TargetingStrategy)
		}
		Expect(strategies).To(ConsistOf(targetSingleFacility, targetMultiSelector))
		Expect(c.errOut()).To(BeEmpty())
	})

	Describe("chunking, because a real catalog import is far larger than 25 pairs", func() {
		It("sends a file that fits in one request", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
				writeJSON(w, http.StatusOK, echoUpsert(body))
			})

			Expect(c.run("listing", "upsert", "--file", tempFile(upsertFile(maxBulkBudget)))).To(Equal(exitcode.OK))

			// Exactly at the limit is not over it.
			Expect(api.calls).To(HaveLen(1))
			Expect(decodeUpsert(api)[0].budget()).To(Equal(maxBulkBudget))
		})

		It("splits a file that does not, keeping every request within the budget", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
				writeJSON(w, http.StatusOK, echoUpsert(body))
			})

			const n = 120

			Expect(c.run("listing", "upsert", "--file", tempFile(upsertFile(n)))).To(Equal(exitcode.OK))

			reqs := decodeUpsert(api)
			Expect(reqs).To(HaveLen(5)) // 120 single-facility entries / 25 per request

			for i, req := range reqs {
				Expect(req.budget()).To(BeNumerically("<=", maxBulkBudget),
					"request %d exceeded the API's limit", i+1)
			}
		})

		It("drops nothing and duplicates nothing: the chunks are exactly the file", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
				writeJSON(w, http.StatusOK, echoUpsert(body))
			})

			const n = 120

			Expect(c.run("listing", "upsert", "--file", tempFile(upsertFile(n)))).To(Equal(exitcode.OK))

			var sent []string
			for _, req := range decodeUpsert(api) {
				sent = append(sent, req.articles()...)
			}

			want := make([]string, n)
			for i := range want {
				want[i] = fmt.Sprintf("a-%d", i)
			}

			// This is the spec that makes chunking safe to trust. A chunker that loses
			// the last partial chunk, or that re-sends the first entry of each, produces
			// a 200 and a silently wrong catalog.
			Expect(sent).To(HaveLen(n))
			Expect(sent).To(ConsistOf(want))

			// And every entry appears in the result table.
			for _, id := range want {
				Expect(c.out()).To(ContainSubstring(id))
			}
		})

		It("counts a multi-selector entry by the facilities it targets, not as one", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
				writeJSON(w, http.StatusOK, echoUpsert(body))
			})

			// Six entries, each writing the same article into five stores: 30 pairs. A
			// chunker that counted entries rather than pairs would send all six in one
			// request and be answered with a 400.
			entries := make([]string, 6)
			for i := range entries {
				entries[i] = fmt.Sprintf(`{
					"targetingStrategy":"MULTI_SELECTOR","tenantArticleId":"m-%d","title":"Article %d",
					"selector":[
						{"facility":{"tenantFacilityId":"S-1"}},
						{"facility":{"tenantFacilityId":"S-2"}},
						{"facility":{"tenantFacilityId":"S-3"}},
						{"facility":{"tenantFacilityId":"S-4"}},
						{"facility":{"tenantFacilityId":"S-5"}}
					]}`, i, i)
			}
			file := tempFile(fmt.Sprintf(`{"listings":[%s]}`, strings.Join(entries, ",")))

			Expect(c.run("listing", "upsert", "--file", file)).To(Equal(exitcode.OK))

			reqs := decodeUpsert(api)
			Expect(len(reqs)).To(BeNumerically(">", 1))

			for i, req := range reqs {
				Expect(req.budget()).To(BeNumerically("<=", maxBulkBudget),
					"request %d exceeded the API's limit", i+1)
			}

			// Still nothing lost.
			var sent []string
			for _, req := range reqs {
				sent = append(sent, req.articles()...)
			}
			Expect(sent).To(ConsistOf("m-0", "m-1", "m-2", "m-3", "m-4", "m-5"))
		})

		It("refuses an entry that alone exceeds the budget rather than half-writing it", func() {
			api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

			selectors := make([]string, maxBulkBudget+1)
			for i := range selectors {
				selectors[i] = fmt.Sprintf(`{"facility":{"tenantFacilityId":"S-%d"}}`, i)
			}
			file := tempFile(fmt.Sprintf(
				`{"listings":[{"targetingStrategy":"MULTI_SELECTOR","tenantArticleId":"m-0","title":"t","selector":[%s]}]}`,
				strings.Join(selectors, ",")))

			Expect(c.run("listing", "upsert", "--file", file)).To(Equal(exitcode.Usage))

			// An entry is atomic: fft will not split one listing's selectors across two
			// requests and leave it written to half its stores.
			Expect(c.errOut()).To(ContainSubstring("targets 26 facilities"))
			Expect(c.errOut()).To(ContainSubstring("at most 25"))
			Expect(api.calls).To(BeEmpty())
		})

		It("passes every field of an entry through, including the ones fft has no model for", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
				writeJSON(w, http.StatusOK, echoUpsert(body))
			})

			file := tempFile(`{"listings":[{
				"targetingStrategy":"SINGLE_FACILITY","tenantArticleId":"4711","title":"t",
				"facility":{"tenantFacilityId":"BER-01"},
				"customAttributes":{"supplierSku":"XYZ-9"},
				"categoryRefs":["shoes"]
			}]}`)

			Expect(c.run("listing", "upsert", "--file", file)).To(Equal(exitcode.OK))

			// fft models four fields of an entry and the schema has three dozen. The
			// entry's own bytes are what is sent, so the rest survive.
			listing := api.only().json()["listings"].([]any)[0].(map[string]any)
			Expect(listing).To(HaveKeyWithValue("customAttributes", HaveKeyWithValue("supplierSku", "XYZ-9")))
			Expect(listing).To(HaveKeyWithValue("categoryRefs", ConsistOf("shoes")))
		})
	})

	Describe("partial success, which is what chunking makes possible", func() {
		It("exits 8 when a chunk is rejected, and keeps going so the rest still land", func() {
			var chunk int

			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
				chunk++
				if chunk == 2 {
					writeJSON(w, http.StatusBadRequest, `[{"summary":"category ref 'shoes' does not exist"}]`)
					return
				}
				writeJSON(w, http.StatusOK, echoUpsert(body))
			})

			Expect(c.run("listing", "upsert", "--file", tempFile(upsertFile(75)))).To(Equal(exitcode.Partial))

			// The chunk after the bad one was still sent: an import of 5,000 listings
			// should not be abandoned because one article has a bad category ref.
			Expect(api.calls).To(HaveLen(3))

			// Every entry of the rejected chunk is FAILED, with the API's own words.
			Expect(c.out()).To(ContainSubstring("FAILED"))
			Expect(c.out()).To(ContainSubstring("category ref 'shoes' does not exist"))

			// And the two chunks that landed are reported as landing.
			Expect(c.out()).To(ContainSubstring("CREATED"))
			Expect(c.errOut()).To(ContainSubstring("25 of 75 listings failed"))
		})

		It("reports a listing the API accepted but never answered for as FAILED", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				// Two entries sent, one reported. The response carries only the listings
				// that were successfully upserted — there is no per-item failure channel —
				// so an entry missing from it is one the API silently did not write.
				writeJSON(w, http.StatusOK, `{
					"listings":[{"status":"CREATED","result":{"tenantArticleId":"a-0","facilityId":"f-1"}}],
					"summary":{"created":1,"updated":0,"unchanged":0}
				}`)
			})

			Expect(c.run("listing", "upsert", "--file", tempFile(upsertFile(2)))).To(Equal(exitcode.Partial))

			Expect(c.out()).To(ContainSubstring("a-1"))
			Expect(c.out()).To(ContainSubstring("reported no result"))
			Expect(c.errOut()).To(ContainSubstring("1 of 2 listings failed"))
		})

		// Regression: the results of the chunks that already landed used to be thrown
		// away when a later chunk aborted the run, leaving the user with no record of
		// what had been written.
		It("still prints the listings that landed when the run is cut short", func() {
			var sent int

			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
				sent++
				if sent == 1 {
					writeJSON(w, http.StatusOK, echoUpsert(body))
					return
				}
				writeJSON(w, http.StatusUnauthorized, `[{"summary":"the token is not valid"}]`)
			})

			Expect(c.run("listing", "upsert", "--file", tempFile(upsertFile(75)))).To(Equal(exitcode.Auth))

			Expect(c.errOut()).To(ContainSubstring("The run stopped early"))
			Expect(c.out()).To(ContainSubstring("a-0"))
			Expect(c.out()).To(ContainSubstring("CREATED"))
		})

		It("stops on a failure that is not about the chunk, rather than repeating it", func() {
			var sent int

			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				sent++
				writeJSON(w, http.StatusUnauthorized, `[{"summary":"the token is not valid"}]`)
			})

			Expect(c.run("listing", "upsert", "--file", tempFile(upsertFile(75)))).To(Equal(exitcode.Auth))

			// A 401 is not "this chunk was bad" — it is "fft cannot talk to the API", and
			// grinding through two more requests to say so twice more helps nobody. (The
			// count is 2 because the client refreshes the token and retries once.)
			Expect(sent).To(BeNumerically("<=", 2))
			Expect(c.out()).To(BeEmpty())
		})
	})

	DescribeTable("refusing an entry the API would answer with a 400 that does not name it",
		func(entry, want string) {
			c := newCLI()
			api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

			file := tempFile(fmt.Sprintf(`{"listings":[%s]}`, entry))

			Expect(c.run("listing", "upsert", "--file", file)).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring(want))
			Expect(api.calls).To(BeEmpty())
		},
		Entry("no targeting strategy",
			`{"tenantArticleId":"4711","title":"t"}`,
			`has no "targetingStrategy"`),
		Entry("an unknown targeting strategy",
			`{"targetingStrategy":"EVERYWHERE","tenantArticleId":"4711","title":"t"}`,
			`unknown targetingStrategy "EVERYWHERE"`),
		Entry("SINGLE_FACILITY without a facility",
			`{"targetingStrategy":"SINGLE_FACILITY","tenantArticleId":"4711","title":"t"}`,
			`is SINGLE_FACILITY but has no "facility"`),
		Entry("MULTI_SELECTOR without a selector",
			`{"targetingStrategy":"MULTI_SELECTOR","tenantArticleId":"4711","title":"t"}`,
			`is MULTI_SELECTOR but has no "selector"`),
		Entry("no article id",
			`{"targetingStrategy":"SINGLE_FACILITY","title":"t","facility":{"tenantFacilityId":"BER-01"}}`,
			"has no tenantArticleId"),
	)

	It("refuses an empty file rather than sending a request that cannot do anything", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("listing", "upsert", "--file", tempFile(`{"listings":[]}`))).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("holds no listings"))
		Expect(api.calls).To(BeEmpty())
	})
})
