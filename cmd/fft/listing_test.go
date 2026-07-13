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

// envelope builds the paginated answer every POST /{entity}/search gives. The
// name of the array is the one part of the shape that is not uniform, so it is a
// parameter; total is omitted when nil, which is what the API does unless the
// search asked for it.
func envelope(items string, entities []string, hasNext bool, endCursor string, total *int) string {
	body := fmt.Sprintf(
		`{%q:[%s],"pageInfo":{"hasNextPage":%t,"endCursor":%q,"hasPreviousPage":false,"startCursor":"a"}`,
		items, strings.Join(entities, ","), hasNext, endCursor)

	if total != nil {
		body += fmt.Sprintf(`,"total":%d`, *total)
	}
	return body + "}"
}

func listingPage(listings ...string) string {
	return envelope("listings", listings, false, "", nil)
}

// listingTenant answers the two requests every per-facility listing command now
// makes: the GET that resolves --facility to its platform id, and the search.
//
// The resolve is not incidental. The listing search can only filter on
// facilityRef, and the index does not match the URN form of a tenantFacilityId
// against it — it returns an empty 200. So --facility BER-01 costs one lookup.
func (c *cli) listingTenant(search func(w http.ResponseWriter, body []byte)) *tenant {
	return c.fakeTenant(func(w http.ResponseWriter, r *http.Request, body []byte) {
		if strings.HasPrefix(r.URL.Path, "/api/facilities/") {
			writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
			return
		}
		search(w, body)
	})
}

// testFacilityID is the platform id of testdata/facility_managed.json, which is
// what --facility BER-01 resolves to.
const testFacilityID = "8f14e45f-ceea-467a-9575-25a1b5c8b3a1"

// searchCall is the listing search, i.e. the request after the resolve.
func (t *tenant) searchCall() call {
	GinkgoHelper()
	Expect(len(t.calls)).To(BeNumerically(">=", 2), "expected a resolve and a search")
	return t.calls[1]
}

var _ = Describe("fft listing list", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	When("the facility has listings", func() {
		var api *tenant

		BeforeEach(func() {
			api = c.listingTenant(func(w http.ResponseWriter, _ []byte) {
				writeJSON(w, http.StatusOK, listingPage(fixture("listing.json"), fixture("listing_inactive.json")))
			})
		})

		It("renders the catalog, dashing the fields a bare listing does not carry", func() {
			Expect(c.run("listing", "list", "--facility", "BER-01")).To(Equal(exitcode.OK))

			Expect(c.out()).To(Equal(strings.Join([]string{
				"TENANT ARTICLE ID   FACILITY                               TITLE              STATUS     PRICE   VERSION",
				"4711                8f14e45f-ceea-467a-9575-25a1b5c8b3a1   Adidas Superstar   ACTIVE     89.95   41",
				"4712                8f14e45f-ceea-467a-9575-25a1b5c8b3a1   -                  INACTIVE   -       3",
				"",
			}, "\n")))
		})

		// Regression (live tenant, 2026-07-12): the listing search filters on
		// facilityRef and the index does NOT match the URN form of a tenantFacilityId
		// against it. Facility 0090000020 holds 760 listings; the URN filter returned
		// **0** — a cheerful empty 200, not an error. So --facility must be resolved to
		// the platform id first, and `purge` (which counts through the same query) would
		// otherwise have reported "no listings; nothing to purge" for a full catalog.
		It("resolves the facility to its platform id and filters facilityRef by that", func() {
			Expect(c.run("listing", "list", "--facility", "BER-01")).To(Equal(exitcode.OK))

			Expect(api.calls).To(HaveLen(2))
			Expect(api.calls[0].Method).To(Equal(http.MethodGet))
			Expect(api.calls[0].Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01"))

			search := api.searchCall()
			Expect(search.Method).To(Equal(http.MethodPost))
			Expect(search.Path).To(Equal("/api/listings/search"))

			// The platform id, never the URN.
			Expect(search.json()).To(HaveKeyWithValue("query", HaveKeyWithValue(
				"facilityRef", HaveKeyWithValue("eq", testFacilityID))))
		})

		It("does not pay for a lookup when it was given the platform id already", func() {
			Expect(c.run("listing", "list", "--facility", testFacilityID)).To(Equal(exitcode.OK))

			Expect(api.only().Path).To(Equal("/api/listings/search"))
			Expect(api.only().json()).To(HaveKeyWithValue("query", HaveKeyWithValue(
				"facilityRef", HaveKeyWithValue("eq", testFacilityID))))
		})

		It("emits the API's own JSON on stdout and leaves stderr clean with -o json", func() {
			Expect(c.run("listing", "list", "--facility", "BER-01", "-o", "json")).To(Equal(exitcode.OK))

			var listings []map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &listings)).To(Succeed())
			Expect(listings).To(HaveLen(2))

			// Full fidelity: a field the table never shows is still there for jq.
			Expect(listings[0]).To(HaveKeyWithValue("imageUrl", "https://example.com/images/4711.jpg"))
			Expect(c.errOut()).To(BeEmpty())
		})
	})

	It("refuses to list without --facility rather than listing the whole tenant", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("listing", "list")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--facility is required"))
		Expect(api.calls).To(BeEmpty())
	})

	It("says so on stderr and prints an empty array when there are none", func() {
		c.listingTenant(func(w http.ResponseWriter, _ []byte) {
			writeJSON(w, http.StatusOK, listingPage())
		})

		Expect(c.run("listing", "list", "--facility", "BER-01", "-o", "json")).To(Equal(exitcode.OK))

		// `| jq length` must answer 0 rather than fail.
		Expect(strings.TrimSpace(c.out())).To(Equal("[]"))
	})

	DescribeTable("refusing a page size the API would reject with an opaque 400",
		func(size string) {
			api := c.listingTenant(func(w http.ResponseWriter, _ []byte) {
				writeJSON(w, http.StatusOK, listingPage())
			})

			Expect(c.run("listing", "list", "--facility", testFacilityID, "--size", size)).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring("page size must be between 1 and 250"))
			Expect(api.calls).To(BeEmpty())
		},
		// Both ends of the range fail the same way. --size 0 fell through as "unset"
		// in M6 and silently became the API's default, while --size 500 was refused.
		Entry("zero", "0"),
		Entry("negative", "-1"),
		Entry("above the maximum", "500"),
	)

	It("refuses --max-items without --all, rather than letting it silently do nothing", func() {
		api := c.listingTenant(func(w http.ResponseWriter, _ []byte) {
			writeJSON(w, http.StatusOK, listingPage())
		})

		// --max-items caps how far the cursor is followed, and without --all there is
		// no cursor. A flag that silently does nothing is found weeks later by someone
		// whose cap has never once applied.
		Expect(c.run("listing", "list", "--facility", testFacilityID, "--max-items", "5")).
			To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--max-items only means something with --all"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft listing get", func() {
	It("addresses the listing by tenantArticleId, not by a platform id", func() {
		c := newCLI()
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("listing.json"))
		})

		Expect(c.run("listing", "get", "--facility", "BER-01", "4711")).To(Equal(exitcode.OK))

		// This is the one entity in the API addressed by the tenant's own article id.
		// The path is the spec: if someone "fixes" the helper to use the listing's
		// platform id (which the entity does carry), this is what says no.
		Expect(api.only().Method).To(Equal(http.MethodGet))
		Expect(api.only().Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01/listings/4711"))
	})

	It("prints the object, not an array of one, so that | jq .price works", func() {
		c := newCLI()
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("listing.json"))
		})

		Expect(c.run("listing", "get", "--facility", "BER-01", "4711", "-o", "json")).To(Equal(exitcode.OK))

		var listing map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &listing)).To(Succeed())
		Expect(listing).To(HaveKeyWithValue("price", BeNumerically("==", 89.95)))
	})
})

var _ = Describe("fft listing patch", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("reads the listing, sends the change as an action, and carries the version it read", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("listing.json"))
		})

		Expect(c.run("listing", "patch", "--facility", "BER-01", "4711", "--status", "INACTIVE")).
			To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))

		patch := api.calls[1]
		Expect(patch.Method).To(Equal(http.MethodPatch))
		Expect(patch.Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01/listings/4711"))

		// The listing PATCH is not a field patch: its body is {version, actions:[…]}
		// and the action carries a discriminator of its own.
		Expect(patch.json()).To(HaveKeyWithValue("version", BeNumerically("==", 41)))
		Expect(patch.json()).To(HaveKeyWithValue("actions", ConsistOf(
			SatisfyAll(
				HaveKeyWithValue("action", "ModifyListing"),
				HaveKeyWithValue("status", "INACTIVE"),
			))))
	})

	It("re-reads and retries exactly once when someone wrote in between", func() {
		var patches int

		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.Method == http.MethodGet {
				version := 41
				if patches > 0 {
					version = 42
				}
				writeJSON(w, http.StatusOK, fmt.Sprintf(
					`{"id":"x","tenantArticleId":"4711","status":"ACTIVE","version":%d}`, version))
				return
			}

			patches++
			if patches == 1 {
				writeJSON(w, http.StatusConflict,
					`[{"summary":"version conflict","version":42,"requestVersion":41}]`)
				return
			}
			writeJSON(w, http.StatusOK, `{"id":"x","tenantArticleId":"4711","status":"INACTIVE","version":43}`)
		})

		Expect(c.run("listing", "patch", "--facility", "BER-01", "4711", "--status", "INACTIVE")).
			To(Equal(exitcode.OK))

		// GET, PATCH(409), GET, PATCH(200) — the change is re-applied to the version
		// that was read second, not replayed against the stale one.
		Expect(api.calls).To(HaveLen(4))
		Expect(api.calls[3].json()).To(HaveKeyWithValue("version", BeNumerically("==", 42)))
	})

	It("gives up after a second conflict rather than looping", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, fixture("listing.json"))
				return
			}
			writeJSON(w, http.StatusConflict,
				`[{"summary":"version conflict","version":99,"requestVersion":41}]`)
		})

		Expect(c.run("listing", "patch", "--facility", "BER-01", "4711", "--status", "INACTIVE")).
			To(Equal(exitcode.Conflict))

		Expect(api.calls).To(HaveLen(4))
		Expect(c.errOut()).To(ContainSubstring("you sent v41, current is v99"))
	})

	It("skips the read when --if-version says what the version is", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("listing.json"))
		})

		Expect(c.run("listing", "patch", "--facility", "BER-01", "4711", "--status", "INACTIVE", "--if-version", "7")).
			To(Equal(exitcode.OK))

		// One request, not two: that is the whole point of the flag.
		Expect(api.only().Method).To(Equal(http.MethodPatch))
		Expect(api.only().json()).To(HaveKeyWithValue("version", BeNumerically("==", 7)))
	})

	It("refuses a patch with nothing in it rather than sending a no-op the API calls success", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("listing", "patch", "--facility", "BER-01", "4711")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("nothing to patch"))
		Expect(api.calls).To(BeEmpty())
	})

	It("sends --price 0, because zero is a price and not an absent flag", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("listing.json"))
		})

		Expect(c.run("listing", "patch", "--facility", "BER-01", "4711", "--price", "0")).To(Equal(exitcode.OK))

		Expect(api.calls[1].json()).To(HaveKeyWithValue("actions", ConsistOf(
			HaveKeyWithValue("price", BeNumerically("==", 0)))))
	})

	It("refuses a negative price before the API answers with a 400 that does not name the field", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("listing", "patch", "--facility", "BER-01", "4711", "--price", "-1")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--price cannot be negative"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft listing delete", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("deletes one listing, addressed by its tenantArticleId", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.WriteHeader(http.StatusNoContent)
		})

		Expect(c.run("listing", "delete", "--facility", "BER-01", "4711", "--yes")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodDelete))
		Expect(api.only().Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01/listings/4711"))

		// Nothing on stdout: a delete has no data, and a script that piped this would
		// otherwise receive a sentence.
		Expect(c.out()).To(BeEmpty())
		Expect(c.errOut()).To(ContainSubstring("Deleted listing 4711"))
	})

	It("refuses on a non-interactive terminal without --yes", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("listing", "delete", "--facility", "BER-01", "4711")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--yes"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft listing purge", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	// countThenPurge answers the count search with total, and the DELETE with a 204.
	countThenPurge := func(c *cli, total int) *tenant {
		return c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			switch r.Method {
			case http.MethodGet:
				writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
			case http.MethodPost:
				writeJSON(w, http.StatusOK, envelope("listings", nil, false, "", &total))
			default:
				w.WriteHeader(http.StatusNoContent)
			}
		})
	}

	// The requests a purge makes: resolve the facility, count, then delete.
	const (
		resolve = 0
		count   = 1
		del     = 2
	)

	It("counts the listings first and echoes the number and the facility in the question", func() {
		c.answer("y")
		api := countThenPurge(c, 4813)

		Expect(c.run("listing", "purge", "--facility", "BER-01")).To(Equal(exitcode.OK))

		// The count is fetched before the question, so the question can name it — and
		// it names the facility the way the user would recognise it. "Purge all 4813
		// listings of facility 8f14e45f-ceea-467a-9575-25a1b5c8b3a1?" is not a question
		// anyone can answer safely.
		Expect(c.errOut()).To(ContainSubstring(
			"Purge all 4813 listings of facility Berlin Mitte (BER-01)?"))

		Expect(api.calls).To(HaveLen(3))
		Expect(api.calls[count].Method).To(Equal(http.MethodPost))
		Expect(api.calls[count].Path).To(Equal("/api/listings/search"))
		Expect(api.calls[count].json()).To(HaveKeyWithValue("options", HaveKeyWithValue("withTotal", true)))

		Expect(api.calls[del].Method).To(Equal(http.MethodDelete))
		Expect(api.calls[del].Path).To(Equal("/api/facilities/" + testFacilityID + "/listings"))

		Expect(c.errOut()).To(ContainSubstring("Purged the listings"))
		Expect(c.errOut()).To(ContainSubstring("4813"))
	})

	It("counts the same set it is about to delete, both by the resolved platform id", func() {
		api := countThenPurge(c, 12)

		Expect(c.run("listing", "purge", "--facility", "BER-01", "--yes")).To(Equal(exitcode.OK))

		// A purge that counted a different set from the one it deletes would be the
		// worst possible bug in this command — and filtering the count by the URN
		// counted *nothing* while the DELETE removed everything. Both spellings are now
		// derived from one resolved id.
		Expect(api.calls[count].json()).To(HaveKeyWithValue("query", HaveKeyWithValue(
			"facilityRef", HaveKeyWithValue("eq", testFacilityID))))
		Expect(api.calls[del].Path).To(ContainSubstring(testFacilityID))
	})

	It("refuses on a non-interactive terminal without --yes, and deletes nothing", func() {
		api := countThenPurge(c, 4813)

		Expect(c.run("listing", "purge", "--facility", "BER-01")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--yes"))

		// It resolved and counted — that is how it phrased the question it could not
		// ask — but it did not delete. A prompt nobody can see is not consent.
		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[count].Method).To(Equal(http.MethodPost))
	})

	It("deletes nothing when the user says no", func() {
		c.answer("n")
		api := countThenPurge(c, 4813)

		Expect(c.run("listing", "purge", "--facility", "BER-01")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("Aborted"))
		Expect(api.calls).To(HaveLen(2))
	})

	It("does not send a DELETE when the facility has no listings", func() {
		api := countThenPurge(c, 0)

		Expect(c.run("listing", "purge", "--facility", "BER-01", "--yes")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("no listings; nothing to purge"))
		Expect(api.calls).To(HaveLen(2))
	})

	It("refuses to purge on a count it does not have", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			switch r.Method {
			case http.MethodGet:
				writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
			case http.MethodPost:
				// withTotal was asked for and the API answered without one. Purging on the
				// strength of a number we do not have is not something to do quietly.
				writeJSON(w, http.StatusOK, envelope("listings", nil, false, "", nil))
			default:
				w.WriteHeader(http.StatusNoContent)
			}
		})

		Expect(c.run("listing", "purge", "--facility", "BER-01", "--yes")).NotTo(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("did not return a total"))
		Expect(api.calls).To(HaveLen(2))
	})

	It("is its own verb: 'delete' cannot be made to purge by forgetting an argument", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		// `delete` without the article id is a usage error, not a catalog wipe.
		Expect(c.run("listing", "delete", "--facility", "BER-01", "--yes")).To(Equal(exitcode.Usage))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft listing set", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("prints an example body without a project, a credential or a request", func() {
		Expect(c.run("listing", "set", "--example")).To(Equal(exitcode.OK))

		var body map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &body)).To(Succeed())
		Expect(body).To(HaveKey("listings"))
		Expect(c.errOut()).To(BeEmpty())
	})

	It("puts the listings and renders the per-item result", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `[
				{"status":"CREATED","listing":{"tenantArticleId":"4711","facilityId":"f-1"}},
				{"status":"UNCHANGED","listing":{"tenantArticleId":"4712","facilityId":"f-1"}}
			]`)
		})

		file := tempFile(`{"listings":[{"tenantArticleId":"4711"},{"tenantArticleId":"4712"}]}`)

		Expect(c.run("listing", "set", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPut))
		Expect(api.only().Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01/listings"))

		Expect(c.out()).To(ContainSubstring("CREATED"))
		Expect(c.out()).To(ContainSubstring("UNCHANGED"))
		Expect(c.errOut()).To(ContainSubstring("2 listings: 1 created, 1 unchanged"))
	})

	It("exits 8 when the API reports an item as FAILED", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `[
				{"status":"CREATED","listing":{"tenantArticleId":"4711","facilityId":"f-1"}},
				{"status":"FAILED","listing":{"tenantArticleId":"4712","facilityId":"f-1"}}
			]`)
		})

		file := tempFile(`{"listings":[{"tenantArticleId":"4711"},{"tenantArticleId":"4712"}]}`)

		Expect(c.run("listing", "set", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.Partial))

		// The table still prints: the CREATED one is real and is not rolled back.
		Expect(c.out()).To(ContainSubstring("4711"))
		Expect(c.out()).To(ContainSubstring("FAILED"))
		Expect(c.errOut()).To(ContainSubstring("1 of 2 listings failed"))
	})

	DescribeTable("refusing a body the all-or-nothing PUT would reject as a block",
		func(body, want string) {
			api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

			Expect(c.run("listing", "set", "--facility", "BER-01", "--file", tempFile(body))).
				To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring(want))
			Expect(api.calls).To(BeEmpty())
		},
		Entry("a bare array", `[{"tenantArticleId":"4711"}]`, `{"listings": [...]}`),
		Entry("an empty set", `{"listings":[]}`, "holds no listings"),
		Entry("an entry with no article id", `{"listings":[{"title":"x"}]}`, "has no tenantArticleId"),
	)

	// Regression: an answer fft could not read used to fall through renderBulk's
	// empty case and print "No listings found." — the exact opposite of what had just
	// happened, after an all-or-nothing PUT that landed.
	It("does not report a landed PUT as 'no listings' when it cannot read the answer", func() {
		c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
				return
			}
			w.WriteHeader(http.StatusOK) // the write landed; the API just said nothing
		})

		file := tempFile(`{"listings":[{"tenantArticleId":"4711"}]}`)

		Expect(c.run("listing", "set", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("The listings were written"))
		Expect(c.errOut()).NotTo(ContainSubstring("No listings found"))
	})

	It("refuses more listings than one request accepts, rather than splitting an atomic write", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		listings := make([]string, maxListingSet+1)
		for i := range listings {
			listings[i] = fmt.Sprintf(`{"tenantArticleId":"a-%d"}`, i)
		}
		file := tempFile(fmt.Sprintf(`{"listings":[%s]}`, strings.Join(listings, ",")))

		Expect(c.run("listing", "set", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.Usage))

		// Chunking here would turn one atomic write into several, which is not what
		// the user asked for. The message names the command that does chunk.
		Expect(c.errOut()).To(ContainSubstring("at most 500"))
		Expect(c.errOut()).To(ContainSubstring("fft listing upsert"))
		Expect(api.calls).To(BeEmpty())
	})
})
