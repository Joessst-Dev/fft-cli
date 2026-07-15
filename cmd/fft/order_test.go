package main

import (
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// order renders one order the way the API does, in the fields the table reads.
func order(id, status, tenantOrderID string, lines, version int) string {
	items := make([]string, lines)
	for i := range items {
		items[i] = `{"quantity":1,"article":{"tenantArticleId":"SKU-1","title":"X"}}`
	}

	tenant := ""
	if tenantOrderID != "" {
		tenant = fmt.Sprintf(`"tenantOrderId":%q,`, tenantOrderID)
	}

	return fmt.Sprintf(
		`{"id":%q,%s"status":%q,"orderDate":"2026-07-15T08:45:50Z","orderLineItems":[%s],"version":%d}`,
		id, tenant, status, strings.Join(items, ","), version)
}

// orderListPage is the envelope GET /api/orders answers with: a total and an
// array, no cursor.
func orderListPage(items []string, total int) string {
	return fmt.Sprintf(`{"orders":[%s],"total":%d}`, strings.Join(items, ","), total)
}

// orderSearchPage is the envelope POST /api/orders/search answers with: a cursor
// in pageInfo, and total only when the search asked for it.
func orderSearchPage(items []string, hasNext bool, endCursor string, total *int) string {
	envelope := fmt.Sprintf(
		`{"orders":[%s],"pageInfo":{"hasNextPage":%t,"endCursor":%q,"hasPreviousPage":false,"startCursor":"a"}`,
		strings.Join(items, ","), hasNext, endCursor)

	if total != nil {
		envelope += fmt.Sprintf(`,"total":%d`, *total)
	}
	return envelope + "}"
}

var _ = Describe("fft order list", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("lists on the GET list, not the search endpoint, and shows the stripped columns", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, orderListPage([]string{
				order("o1", orderStatusOpen, "ORD-1", 2, 3),
				order("o2", orderStatusCancelled, "", 1, 7),
			}, 2))
		})

		Expect(c.run("order", "list")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodGet))
		Expect(api.only().Path).To(Equal("/api/orders"))

		out := c.out()
		Expect(out).To(ContainSubstring("ID   TENANT ID   STATUS"))
		Expect(out).To(ContainSubstring("o1   ORD-1"))
		Expect(out).To(ContainSubstring("OPEN"))
		// The line count is the number of orderLineItems, not a field the API sends.
		Expect(out).To(MatchRegexp(`o1\s+ORD-1\s+OPEN\s+\S+\s+2\s+3`))
		// A stripped order the API sent no tenantOrderId for is a dash, not a blank.
		Expect(out).To(MatchRegexp(`o2\s+-\s+CANCELLED`))
	})

	It("passes --tenant-order-id and --consumer-id as query filters", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, orderListPage(nil, 0))
		})

		Expect(c.run("order", "list", "--tenant-order-id", "ORD-9", "--consumer-id", "C-4711")).To(Equal(exitcode.OK))

		Expect(api.only().Query.Get("tenantOrderId")).To(Equal("ORD-9"))
		Expect(api.only().Query.Get("consumerId")).To(Equal("C-4711"))
	})

	It("starts the next page after the last id, since this endpoint has no cursor", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.URL.Query().Get("startAfterId") == "o2" {
				writeJSON(w, http.StatusOK, orderListPage([]string{
					order("o3", orderStatusOpen, "ORD-3", 1, 1),
				}, 3))
				return
			}
			writeJSON(w, http.StatusOK, orderListPage([]string{
				order("o1", orderStatusOpen, "ORD-1", 1, 1),
				order("o2", orderStatusOpen, "ORD-2", 1, 1),
			}, 3))
		})

		Expect(c.run("order", "list", "--all", "--size", "2")).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[0].Query).NotTo(HaveKey("startAfterId"))
		Expect(api.calls[1].Query.Get("startAfterId")).To(Equal("o2"))
		Expect(c.out()).To(ContainSubstring("o3"))
	})

	It("says there are more when the total outruns the page, without fetching them", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, orderListPage([]string{
				order("o1", orderStatusOpen, "ORD-1", 1, 1),
			}, 9))
		})

		Expect(c.run("order", "list", "--size", "1")).To(Equal(exitcode.OK))
		Expect(c.errOut()).To(ContainSubstring("There are more orders. Pass --all"))
	})

	It("emits the API's own JSON on stdout with -o json", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, orderListPage([]string{
				order("o1", orderStatusOpen, "ORD-1", 1, 1),
			}, 1))
		})

		Expect(c.run("order", "list", "-o", "json")).To(Equal(exitcode.OK))
		Expect(c.out()).To(ContainSubstring(`"id": "o1"`))
		Expect(c.errOut()).To(BeEmpty())
	})
})

var _ = Describe("fft order search", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("searches the cursor endpoint and builds the status, sort and date filters", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, orderSearchPage([]string{
				order("o1", orderStatusLocked, "ORD-1", 1, 2),
			}, false, "", nil))
		})

		Expect(c.run("order", "search",
			"--status", "OPEN", "--status", "LOCKED",
			"--sort", "orderDate:desc",
			"--since", "2026-07-01", "--until", "2026-07-15")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/orders/search"))

		payload := api.only().json()
		Expect(payload).To(HaveKeyWithValue("query",
			HaveKeyWithValue("status", HaveKeyWithValue("in", ConsistOf("OPEN", "LOCKED")))))
		Expect(payload).To(HaveKeyWithValue("query",
			HaveKeyWithValue("orderDate", And(HaveKey("gte"), HaveKey("lte")))))
		Expect(payload).To(HaveKeyWithValue("sort",
			ContainElement(HaveKeyWithValue("orderDate", "DESC"))))
	})

	It("refuses an unknown status before anything goes over the wire", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + ", but the status should have been refused first")
		})

		Expect(c.run("order", "search", "--status", "BOGUS")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("unknown --status"))
		Expect(api.calls).To(BeEmpty())
	})

	It("refuses an unparseable --since before anything goes over the wire", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + ", but the date should have been refused first")
		})

		Expect(c.run("order", "search", "--since", "last tuesday")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("--since"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft order get", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("renders one order as an object, not a one-element array", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, order("o1", orderStatusOpen, "ORD-1", 1, 3))
		})

		Expect(c.run("order", "get", "o1", "-o", "json")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodGet))
		Expect(api.only().Path).To(Equal("/api/orders/o1"))

		// An object, so `get -o json | jq .status` works — not `.[0].status`.
		out := strings.TrimSpace(c.out())
		Expect(out).To(HavePrefix("{"))
		Expect(out).To(ContainSubstring(`"status": "OPEN"`))
	})

	It("exits 6 when the order does not exist", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusNotFound, `[{"message":"not found"}]`)
		})

		Expect(c.run("order", "get", "missing")).To(Equal(exitcode.NotFound))
	})
})

var _ = Describe("fft order create", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("prints a valid OrderForCreation for --example with no project or network", func() {
		Expect(c.run("order", "create", "--example")).To(Equal(exitcode.OK))

		doc, err := decodeDoc([]byte(c.out()), "the example")
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(HaveKey("orderDate"))
		Expect(doc).To(HaveKey("consumer"))
		Expect(doc).To(HaveKeyWithValue("orderLineItems", HaveLen(1)))
	})

	It("sends the body verbatim and reports the id the API minted", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, order("o-new", orderStatusOpen, "ORD-1", 1, 1))
		})

		file := tempFile(orderCreateExample)
		Expect(c.run("order", "create", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/orders"))
		Expect(api.only().json()).To(HaveKeyWithValue("tenantOrderId", "ORD-2026-0001"))
		Expect(c.errOut()).To(ContainSubstring("Created order o-new"))
	})

	It("requires --file", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " for a create with no body")
		})

		Expect(c.run("order", "create")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("--file is required"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft order update", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("reads the current version and PATCHes it back", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, order("o1", orderStatusOpen, "ORD-1", 1, 4))
		})

		file := tempFile(`{"comment":"edit"}`)
		Expect(c.run("order", "update", "o1", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[0].Method).To(Equal(http.MethodGet))

		patch := api.calls[1]
		Expect(patch.Method).To(Equal(http.MethodPatch))
		Expect(patch.Path).To(Equal("/api/orders/o1"))
		Expect(patch.json()).To(HaveKeyWithValue("version", BeNumerically("==", 4)))
		Expect(patch.json()).To(HaveKeyWithValue("comment", "edit"))
	})

	It("skips the read when --if-version says what the version is", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, order("o1", orderStatusOpen, "ORD-1", 1, 9))
		})

		file := tempFile(`{"comment":"edit"}`)
		Expect(c.run("order", "update", "o1", "--file", file, "--if-version", "7")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPatch))
		Expect(api.only().json()).To(HaveKeyWithValue("version", BeNumerically("==", 7)))
	})

	It("requires --file", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " for an update with no body")
		})

		Expect(c.run("order", "update", "o1")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("--file is required"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft order cancel", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("reads the version and posts a CANCEL action carrying it", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, order("o1", orderStatusOpen, "ORD-1", 1, 4))
				return
			}
			writeJSON(w, http.StatusOK, order("o1", orderStatusCancelled, "ORD-1", 1, 5))
		})

		Expect(c.run("order", "cancel", "o1", "--reason-id", "out-of-stock", "--yes")).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[0].Method).To(Equal(http.MethodGet))

		action := api.calls[1]
		Expect(action.Method).To(Equal(http.MethodPost))
		Expect(action.Path).To(Equal("/api/orders/o1/actions"))
		Expect(action.json()).To(HaveKeyWithValue("name", "CANCEL"))
		Expect(action.json()).To(HaveKeyWithValue("version", BeNumerically("==", 4)))
		Expect(action.json()).To(HaveKeyWithValue("cancelationReasonId", "out-of-stock"))
		Expect(c.errOut()).To(ContainSubstring("Cancelled order o1"))
	})

	It("sends FORCE_CANCEL under --force", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, order("o1", orderStatusCancelled, "ORD-1", 1, 4))
		})

		Expect(c.run("order", "cancel", "o1", "--force", "--yes")).To(Equal(exitcode.OK))

		action := api.calls[len(api.calls)-1]
		Expect(action.json()).To(HaveKeyWithValue("name", "FORCE_CANCEL"))
	})

	It("refuses --reason-id together with --force, sending nothing", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + ", but the flags should have been refused first")
		})

		Expect(c.run("order", "cancel", "o1", "--force", "--reason-id", "x", "--yes")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("cannot be combined with --force"))
		Expect(api.calls).To(BeEmpty())
	})

	It("does not cancel when the answer is no", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " after a declined confirmation")
		})
		c.answer("n")

		Expect(c.run("order", "cancel", "o1")).To(Equal(exitcode.OK))
		Expect(c.errOut()).To(ContainSubstring("was not cancelled"))
		Expect(api.calls).To(BeEmpty())
	})

	It("surfaces a version conflict as exit 7", func() {
		c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, order("o1", orderStatusOpen, "ORD-1", 1, 4))
				return
			}
			writeJSON(w, http.StatusConflict, `[{"message":"stale version"}]`)
		})

		Expect(c.run("order", "cancel", "o1", "--yes")).To(Equal(exitcode.Conflict))
	})
})

var _ = Describe("fft order unlock", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("reads the version and posts an UNLOCK action with the target time", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, order("o1", orderStatusLocked, "ORD-1", 1, 2))
				return
			}
			writeJSON(w, http.StatusOK, order("o1", orderStatusOpen, "ORD-1", 1, 3))
		})

		Expect(c.run("order", "unlock", "o1", "--target-time", "2026-07-20T12:00:00Z")).To(Equal(exitcode.OK))

		action := api.calls[len(api.calls)-1]
		Expect(action.Method).To(Equal(http.MethodPost))
		Expect(action.Path).To(Equal("/api/orders/o1/actions"))
		Expect(action.json()).To(HaveKeyWithValue("name", "UNLOCK"))
		Expect(action.json()).To(HaveKeyWithValue("version", BeNumerically("==", 2)))
		Expect(action.json()).To(HaveKeyWithValue("targetTime", "2026-07-20T12:00:00Z"))
		Expect(c.errOut()).To(ContainSubstring("Unlocked order o1"))
	})
})
