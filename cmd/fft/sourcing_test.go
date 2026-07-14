package main

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// twoOptions is a routing answer with the *worse* option first, so that a table which
// merely echoed the API's order would look right and be wrong. The second option also
// leaves an item behind.
const twoOptions = `{
  "id": "run-1",
  "result": {
    "options": [
      {
        "id": "opt-B", "runId": "run-1", "totalPenalty": 412,
        "nodes": [
          {"id":"n1","type":"MANAGED_FACILITY","tenantFacilityId":"FRA-02","lineItems":[{"tenantArticleId":"A1","quantity":1}]},
          {"id":"n2","type":"CUSTOMER","lineItems":[{"tenantArticleId":"A1","quantity":1}]}
        ],
        "transfers": [{"sourceNodeRef":"n1","targetNodeRef":"n2","facilityConnectionRef":"c1","lineItems":[],"packagingInformation":[]}],
        "nonAssignedOrderLineItems": [{"tenantArticleId":"A2","quantity":2}],
        "ratingResults": [],
        "totalCosts": {"totalCosts":{"value":129900,"currency":"EUR","decimalPlaces":2}},
        "estimatedDeliveryDate": "2026-07-20",
        "validUntil": "2026-07-15T10:00:00Z"
      },
      {
        "id": "opt-A", "runId": "run-1", "totalPenalty": 187,
        "nodes": [
          {"id":"n1","type":"MANAGED_FACILITY","tenantFacilityId":"BER-01","lineItems":[{"tenantArticleId":"A1","quantity":3}]},
          {"id":"n2","type":"CUSTOMER","lineItems":[{"tenantArticleId":"A1","quantity":3}]}
        ],
        "transfers": [{"sourceNodeRef":"n1","targetNodeRef":"n2","facilityConnectionRef":"c3","lineItems":[],"packagingInformation":[]}],
        "ratingResults": [],
        "totalCosts": {"totalCosts":{"value":25000,"currency":"EUR","decimalPlaces":2}},
        "estimatedDeliveryDate": "2026-07-18",
        "validUntil": "2026-07-15T10:00:00Z"
      }
    ]
  }
}`

// notRoutable is what the API says when there is no way to fulfil the order at all.
// It is a 201, and its options array is empty.
const notRoutable = `{"id":"run-empty","result":{"options":[]}}`

var _ = Describe("fft sourcing simulate", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	Describe("the table", func() {
		BeforeEach(func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusCreated, twoOptions)
			})
		})

		// The penalty is a *penalty*: lower is better. The API returned opt-B (412)
		// first, and a table that showed it first would be recommending the worse option
		// while looking entirely plausible.
		It("sorts by penalty ascending, whatever order the API used", func() {
			file := tempFile(sourcingExample)
			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

			Expect(c.out()).To(Equal(strings.Join([]string{
				"#   PENALTY   ROUTE               SOURCED   UNSOURCED   COST          ETA          VALID UNTIL            ID",
				"1   187       BER-01 → customer   3         0           250.00 EUR    2026-07-18   2026-07-15T10:00:00Z   opt-A",
				"2   412       FRA-02 → customer   1         2           1299.00 EUR   2026-07-20   2026-07-15T10:00:00Z   opt-B",
				"",
			}, "\n")))
		})

		// 25000 minor units with decimalPlaces 2 is 250.00, not twenty-five thousand.
		It("scales money out of its smallest subunit", func() {
			file := tempFile(sourcingExample)
			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("250.00 EUR"))
			Expect(c.out()).NotTo(ContainSubstring("25000"))
		})

		// An option can come back looking healthy and still have quietly dropped items.
		It("warns that an option leaves items unsourced", func() {
			file := tempFile(sourcingExample)
			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("cannot source every item"))
		})

		It("reports the run id, on stderr, so the answer can be read back", func() {
			file := tempFile(sourcingExample)
			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("fft sourcing get run-1"))
			Expect(c.out()).NotTo(ContainSubstring("run-1"), "the notice belongs on stderr, not in the pipe")
		})
	})

	// The single biggest trap in this domain. An empty options array is a *success*,
	// and it does not mean "nothing matched" — it means the router cannot fulfil this
	// order from anywhere. A bland empty table would read exactly like a query with no
	// hits.
	Describe("an order that cannot be routed", func() {
		BeforeEach(func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusCreated, notRoutable)
			})
		})

		It("says the order cannot be routed, rather than printing an empty table", func() {
			file := tempFile(sourcingExample)
			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("found no way to fulfil this order"))
		})

		It("leaves stdout empty, so a pipe receives nothing rather than a word", func() {
			file := tempFile(sourcingExample)
			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

			Expect(c.out()).To(BeEmpty())
		})

		// The answer to a sourcing run is a *document*, not a list. The empty-list
		// rendering — a bare `[]` — is right for `fft facility list` and catastrophic
		// here: it would throw away the run id that the notice on stderr has just told
		// the user to pass to `fft sourcing get`, and it would break the very jq recipe
		// the skill ships for this case, which indexes `.result.options`.
		It("still prints the API's document under -o json, run id and all", func() {
			file := tempFile(sourcingExample)
			Expect(c.run("sourcing", "simulate", "--file", file, "-o", "json")).To(Equal(exitcode.OK))

			Expect(c.out()).NotTo(Equal("[]\n"), "an empty option list is not an empty list command")
			Expect(c.out()).To(ContainSubstring("run-empty"))

			var doc map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &doc)).To(Succeed())
			Expect(doc).To(HaveKey("result"))
		})
	})

	// Click-and-collect with a hop: the goods move BER-01 → FRA-02 and the consumer
	// collects at FRA-02. There are transfers, and there is no CUSTOMER node at all.
	// Counting the customer would answer 0 here, and an option that claims to move
	// nothing while also dropping nothing describes no possible world.
	It("counts what a collect-at-another-store option sources, which has no customer node", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, `{
              "id": "run-cc",
              "result": {"options": [{
                "id": "opt-cc", "runId": "run-cc", "totalPenalty": 12,
                "nodes": [
                  {"id":"n1","type":"MANAGED_FACILITY","tenantFacilityId":"BER-01","lineItems":[{"tenantArticleId":"A1","quantity":4}]},
                  {"id":"n2","type":"MANAGED_FACILITY","tenantFacilityId":"FRA-02","isPickUpLocation":true,"lineItems":[{"tenantArticleId":"A1","quantity":4}]}
                ],
                "transfers": [{"sourceNodeRef":"n1","targetNodeRef":"n2","facilityConnectionRef":"c1","lineItems":[],"packagingInformation":[]}],
                "ratingResults": [],
                "estimatedPickupDate": "2026-07-19"
              }]}
            }`)
		})

		file := tempFile(sourcingExample)
		Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

		// Four items sourced, none dropped — not the "0 sourced, 0 unsourced" that
		// counting the (absent) customer node would have produced.
		Expect(c.out()).To(MatchRegexp(`BER-01 → FRA-02\s+4\s+0\s`))

		// And it is a collection, not a delivery: they are different promises.
		Expect(c.out()).To(ContainSubstring("2026-07-19 (pickup)"))
	})

	// The goods are counted where they come to rest, so a two-hop route must not claim
	// to have sourced the order twice.
	It("does not count an item once per leg it travels", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, `{
              "id": "run-2", "result": {"options": [{
                "id": "opt-2", "runId": "run-2", "totalPenalty": 5,
                "nodes": [
                  {"id":"n1","type":"SUPPLIER","tenantFacilityId":"SUP-1","lineItems":[{"tenantArticleId":"A1","quantity":2}]},
                  {"id":"n2","type":"MANAGED_FACILITY","tenantFacilityId":"BER-01","lineItems":[{"tenantArticleId":"A1","quantity":2}]},
                  {"id":"n3","type":"CUSTOMER","lineItems":[{"tenantArticleId":"A1","quantity":2}]}
                ],
                "transfers": [
                  {"sourceNodeRef":"n1","targetNodeRef":"n2","lineItems":[],"packagingInformation":[]},
                  {"sourceNodeRef":"n2","targetNodeRef":"n3","lineItems":[],"packagingInformation":[]}
                ],
                "ratingResults": []
              }]}
            }`)
		})

		file := tempFile(sourcingExample)
		Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

		// Two items, three nodes, two legs — and the answer is 2, not 6.
		Expect(c.out()).To(MatchRegexp(`SUP-1 → BER-01 → customer\s+2\s+0\s`))
	})

	Describe("the optimisation knobs", func() {
		var api *tenant

		BeforeEach(func() {
			api = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusCreated, twoOptions)
			})
		})

		// The API's own default is 1. A user who typed "show me my options" and got
		// exactly one has been answered with a number, not with options.
		It("asks for more than the API's default of one option", func() {
			file := tempFile(sourcingExample)
			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

			hints := api.only().json()["optimizationHints"].(map[string]any)
			Expect(hints).To(HaveKeyWithValue("requestedResultCount", BeNumerically("==", defaultResults)))
		})

		// A file that asked for ten options and a user who did not type --results should
		// get ten. The default is a default, not an override.
		It("leaves a count the file already asked for alone", func() {
			file := tempFile(`{"optimizationHints":{"requestedResultCount":10},` +
				`"order":{"consumer":{},"orderLineItems":[]}}`)

			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))

			hints := api.only().json()["optimizationHints"].(map[string]any)
			Expect(hints).To(HaveKeyWithValue("requestedResultCount", BeNumerically("==", 10)))
		})

		It("overrides the file when --results was actually typed", func() {
			file := tempFile(`{"optimizationHints":{"requestedResultCount":10},` +
				`"order":{"consumer":{},"orderLineItems":[]}}`)

			Expect(c.run("sourcing", "simulate", "--file", file, "--results", "2")).To(Equal(exitcode.OK))

			hints := api.only().json()["optimizationHints"].(map[string]any)
			Expect(hints).To(HaveKeyWithValue("requestedResultCount", BeNumerically("==", 2)))
		})

		It("sends the resource investment as the API takes it", func() {
			file := tempFile(sourcingExample)
			Expect(c.run("sourcing", "simulate", "--file", file, "--investment", "0.5")).To(Equal(exitcode.OK))

			investment := api.only().json()["resourceInvestment"].(map[string]any)
			Expect(investment).To(HaveKeyWithValue("level", BeNumerically("==", 0.5)))
		})
	})

	Describe("the values the API would reject with an opaque 400", func() {
		var api *tenant

		BeforeEach(func() {
			api = c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
				Fail("fft sent " + r.Method + " " + r.URL.Path + ", but the flag should have been refused first")
			})
		})

		AfterEach(func() {
			Expect(api.calls).To(BeEmpty(), "nothing should have gone over the wire")
		})

		It("refuses more results than the spec allows", func() {
			file := tempFile(sourcingExample)

			Expect(c.run("sourcing", "simulate", "--file", file, "--results", "21")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("between 1 and 20"))
		})

		// The spec's minimum is *exclusive*, so --investment 0 is a 400 rather than "do
		// not bother optimising".
		It("refuses an investment of zero, which the spec excludes", func() {
			file := tempFile(sourcingExample)

			Expect(c.run("sourcing", "simulate", "--file", file, "--investment", "0")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("above 0"))
		})

		It("refuses an order with no consumer, which is the one field the API requires", func() {
			file := tempFile(`{"order":{"orderLineItems":[]}}`)

			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("where the order is going"))
		})
	})

	Describe("--example", func() {
		It("prints an order that its own validation accepts", func() {
			Expect(c.run("sourcing", "simulate", "--example")).To(Equal(exitcode.OK))

			file := tempFile(c.out())

			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusCreated, twoOptions)
			})
			Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))
		})

		It("needs no project, no credentials and no network", func() {
			Expect(c.run("sourcing", "simulate", "--example")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"consumer"`))
		})
	})
})

// The classification this whole feature turns on, and the one a future reader is most
// likely to get wrong. createSourcingOptionsRequest is a POST — but it reserves no
// stock, creates no order and changes nothing, so internal/api/access.go lists it as a
// read. An agent or a CI job exploring a production tenant with --read-only must be
// able to ask the router what it would do.
var _ = Describe("sourcing and the read-only gate", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("simulates on a read-only project, because a simulation is not a write", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, twoOptions)
		})
		c.setenv(config.EnvReadOnly, "1")

		file := tempFile(sourcingExample)
		Expect(c.run("sourcing", "simulate", "--file", file)).To(Equal(exitcode.OK))
		Expect(c.out()).To(ContainSubstring("opt-A"))
	})

	// The other side of the same coin: a connection delete really is a write, and the
	// gate stops it before anything leaves the machine.
	It("still refuses to delete a connection on a read-only project", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " on a read-only project")
		})
		c.setenv(config.EnvReadOnly, "1")

		Expect(c.run("connection", "delete", "c2", "--facility", "BER-01", "--yes")).To(Equal(exitcode.ReadOnly))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft sourcing get", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("reads a run back by its id", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, twoOptions)
		})

		Expect(c.run("sourcing", "get", "run-1")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodGet))
		Expect(api.only().Path).To(Equal("/api/routing/sourcingoptions/run-1"))
		Expect(c.out()).To(ContainSubstring("opt-A"))
	})

	// The join back to the other half of this release: the router names the edge it
	// chose, and `fft connection get` says what that edge is.
	It("keeps the facilityConnectionRef of every transfer in -o json", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, twoOptions)
		})

		Expect(c.run("sourcing", "get", "run-1", "-o", "json")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring("facilityConnectionRef"))
		Expect(c.out()).To(ContainSubstring("c3"))
	})
})
