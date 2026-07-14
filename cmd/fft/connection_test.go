package main

import (
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// connection renders one connection the way the API does: with the type inside the
// target and *not* at the top level, which is the asymmetry connectionBody exists to
// absorb.
func connection(id, typ, target, carrier string, version int) string {
	inTarget := fmt.Sprintf(`{"type":%q}`, typ)
	if target != "" {
		inTarget = fmt.Sprintf(`{"type":%q,"facilityRef":%q}`, typ, target)
	}

	return fmt.Sprintf(
		`{"id":%q,"sourceFacilityRef":"ber-uuid","carrierKey":%q,"carrierName":"DHL","version":%d,`+
			`"target":%s,"fallbackTransitTime":{"minTransitDays":1,"maxTransitDays":3}}`,
		id, carrier, version, inTarget)
}

// connectionPage is the envelope GET /api/facilities/{id}/connections answers with.
// It has no pageInfo and no cursor: a total, and an array.
func connectionPage(items []string, total int) string {
	return fmt.Sprintf(`{"interFacilityConnections":[%s],"total":%d}`, strings.Join(items, ","), total)
}

var _ = Describe("fft connection list", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("renders all three kinds of connection in one table", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connectionPage([]string{
				connection("c1", typeManagedFacility, "fra-uuid", "DHL_V2", 3),
				connection("c2", typeSupplier, "sup-uuid", "UPS", 1),
				connection("c3", typeCustomer, "", "DHL_V2", 7),
			}, 3))
		})

		Expect(c.run("connection", "list", "--facility", "BER-01")).To(Equal(exitcode.OK))

		// The customer target has no facilityRef and is not *missing* one — the consumer
		// is not a facility. So it says so, rather than showing the dash that means "the
		// API did not send one".
		Expect(c.out()).To(Equal(strings.Join([]string{
			"ID   TYPE               TARGET           CARRIER   TRANSIT   CONTEXT   VERSION",
			"c1   MANAGED_FACILITY   fra-uuid         DHL_V2    1–3 d     -         3",
			"c2   SUPPLIER           sup-uuid         UPS       1–3 d     -         1",
			"c3   CUSTOMER           (the customer)   DHL_V2    1–3 d     -         7",
			"",
		}, "\n")))
	})

	It("addresses the facility in the path, where the URN form works", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connectionPage(nil, 0))
		})

		Expect(c.run("connection", "list", "--facility", "BER-01")).To(Equal(exitcode.OK))

		// One request: a path parameter takes the URN, so there is nothing to look up.
		Expect(api.only().Method).To(Equal(http.MethodGet))
		Expect(api.only().Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01/connections"))
	})

	// The regression this spec exists for is a *silent* one. A query filter does not
	// resolve a URN: the API answers one it cannot resolve with a cheerful, empty 200,
	// which reads as "this facility has no connections there" rather than as "you asked
	// the wrong question". So --target must be resolved to a platform id first, even
	// though --facility on the very same command line must not be.
	It("resolves --target to a platform id before using it as a query filter", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if !strings.HasSuffix(r.URL.Path, "/connections") {
				writeJSON(w, http.StatusOK, `{"id":"fra-uuid","name":"Frankfurt","tenantFacilityId":"FRA-02","version":1}`)
				return
			}
			writeJSON(w, http.StatusOK, connectionPage(nil, 0))
		})

		Expect(c.run("connection", "list", "--facility", "BER-01", "--target", "FRA-02")).To(Equal(exitcode.OK))

		// Two requests: the lookup that turns FRA-02 into a UUID, then the list.
		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[0].Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:FRA-02"))

		list := api.calls[1]
		Expect(list.Query.Get("targetFacilityRef")).To(Equal("fra-uuid"))
		Expect(list.Query.Get("targetFacilityRef")).NotTo(HavePrefix("urn:"),
			"a URN in a query filter is answered with an empty 200, not an error")
	})

	It("requires --facility, because a connection has no tenant-wide address", func() {
		Expect(c.run("connection", "list")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("--facility is required"))
	})

	Describe("paging by startAfterId", func() {
		// This endpoint has no cursor and no hasNextPage. The next page starts after the
		// last id of this one, and a short page is the only end-of-list signal there is.
		It("starts the next page after the last id of the one before", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
				if r.URL.Query().Get("startAfterId") == "c2" {
					writeJSON(w, http.StatusOK, connectionPage([]string{
						connection("c3", typeCustomer, "", "DHL_V2", 1),
					}, 3))
					return
				}
				writeJSON(w, http.StatusOK, connectionPage([]string{
					connection("c1", typeSupplier, "sup-uuid", "UPS", 1),
					connection("c2", typeSupplier, "sup-uuid", "UPS", 1),
				}, 3))
			})

			Expect(c.run("connection", "list", "--facility", "BER-01", "--all", "--size", "2")).To(Equal(exitcode.OK))

			Expect(api.calls).To(HaveLen(2))
			Expect(api.calls[0].Query).NotTo(HaveKey("startAfterId"))
			Expect(api.calls[1].Query.Get("startAfterId")).To(Equal("c2"))
			Expect(c.out()).To(ContainSubstring("c3"))
		})

		It("says there are more when the total outruns the page, and does not fetch them", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, connectionPage([]string{
					connection("c1", typeSupplier, "sup-uuid", "UPS", 1),
				}, 9))
			})

			Expect(c.run("connection", "list", "--facility", "BER-01", "--size", "1")).To(Equal(exitcode.OK))

			Expect(api.calls).To(HaveLen(1))
			Expect(c.errOut()).To(ContainSubstring("There are more connections. Pass --all"))
		})

		// A truncated list that does not say it was truncated is a wrong answer that
		// looks like a right one.
		It("truncates at --max-items and warns on stderr, while still exiting 0", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, connectionPage([]string{
					connection("c1", typeSupplier, "sup-uuid", "UPS", 1),
					connection("c2", typeSupplier, "sup-uuid", "UPS", 1),
				}, 99))
			})

			Expect(c.run("connection", "list", "--facility", "BER-01",
				"--all", "--size", "2", "--max-items", "2")).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("stopped after 2 items"))
			Expect(c.out()).To(ContainSubstring("c1"))
		})

		It("reports the total only when it was asked for", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, connectionPage([]string{
					connection("c1", typeSupplier, "sup-uuid", "UPS", 1),
				}, 1))
			})

			Expect(c.run("connection", "list", "--facility", "BER-01")).To(Equal(exitcode.OK))
			Expect(c.errOut()).NotTo(ContainSubstring("Total:"))

			Expect(c.run("connection", "list", "--facility", "BER-01", "--total")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("Total: 1"))
		})
	})
})

var _ = Describe("fft connection create", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	Describe("--example", func() {
		// The synthesized body for this operation contradicts itself — it says CUSTOMER
		// at the top level and SUPPLIER in the target, and names no facility for the
		// supplier to be. So the examples are hand-written, and the thing worth pinning
		// is that each one is a body fft itself would accept.
		It("prints a body that its own validation accepts, for every type", func() {
			for _, typ := range connectionTypes() {
				Expect(c.run("connection", "create", "--example", "--type", typ)).To(Equal(exitcode.OK))

				doc, err := decodeDoc([]byte(c.out()), "the example")
				Expect(err).NotTo(HaveOccurred())
				Expect(normaliseConnectionType(doc)).To(Succeed(),
					"the --type %s example is a body fft would refuse", typ)
				Expect(docString(doc, "type")).To(Equal(typ))
			}
		})

		It("needs no project, no credentials and no network", func() {
			// No fakeTenant, no headless: --example is answered before any of that.
			Expect(c.run("connection", "create", "--example")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"type": "SUPPLIER"`))
		})

		It("refuses a type the API does not have", func() {
			Expect(c.run("connection", "create", "--example", "--type", "WAREHOUSE")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("unknown --type"))
		})

		// --type only chooses which example to print. The type of a connection fft
		// *sends* comes from the body, because that is where the API reads it — so
		// --type alongside --file is refused rather than quietly ignored. A flag that
		// silently does nothing is found out weeks later by somebody whose connections
		// have all been suppliers.
		It("refuses --type alongside --file, rather than ignoring it", func() {
			file := tempFile(connectionSupplierExample)

			Expect(c.run("connection", "create", "--facility", "BER-01",
				"--file", file, "--type", "CUSTOMER")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("none of the others can be"))
		})
	})

	It("sends the body and reports the id the API minted", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connection("c-new", typeSupplier, "sup-uuid", "DHL_V2", 1))
		})

		file := tempFile(connectionSupplierExample)
		Expect(c.run("connection", "create", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01/connections"))
		Expect(c.errOut()).To(ContainSubstring("Created connection c-new"))
	})

	Describe("the checks the API's own 400 will not make for you", func() {
		// A tenant that fails the spec if it is ever reached. Every refusal below has to
		// happen *before* the request — a body fft cannot spell correctly is worth more
		// than a 400 delivered quickly, and a check that only runs after the round trip
		// is not a check at all.
		var api *tenant

		BeforeEach(func() {
			api = c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
				Fail("fft sent " + r.Method + " " + r.URL.Path + ", but the body should have been refused first")
			})
		})

		AfterEach(func() {
			Expect(api.calls).To(BeEmpty(), "nothing should have gone over the wire")
		})

		It("refuses a body whose discriminator disagrees with its target", func() {
			file := tempFile(`{"type":"CUSTOMER","target":{"type":"SUPPLIER","facilityRef":"sup-uuid"}}`)

			Expect(c.run("connection", "create", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("must agree"))
		})

		It("refuses a supplier connection that names no facility to connect to", func() {
			file := tempFile(`{"type":"SUPPLIER","target":{"type":"SUPPLIER"}}`)

			Expect(c.run("connection", "create", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring(`needs "facilityRef"`))
		})

		It("refuses a body with no target at all", func() {
			file := tempFile(`{"type":"SUPPLIER"}`)

			Expect(c.run("connection", "create", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring(`no "target"`))
		})
	})

	// The other half of the facilityRef check: a CUSTOMER target has none, is not
	// missing one, and must not be asked for one.
	It("accepts a customer connection with a bare target", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connection("c-new", typeCustomer, "", "DHL_V2", 1))
		})

		file := tempFile(`{"type":"CUSTOMER","target":{"type":"CUSTOMER"}}`)
		Expect(c.run("connection", "create", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.OK))
	})
})

var _ = Describe("fft connection update", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	// The round trip is the whole point of curating this command. The connection the
	// API *returns* carries its type only inside the target; the body it *accepts*
	// requires one at the top level too. So `get -o json | update --file -` would send
	// a body with no discriminator and collect a 400 that names no field — unless fft
	// fills it in, which is what this pins.
	It("completes a body that came straight out of `connection get`, which has no top-level type", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connection("c2", typeSupplier, "sup-uuid", "UPS", 4))
			_ = r
		})

		// Exactly what the API returns: no "type" at the top level.
		asRead := connection("c2", typeSupplier, "sup-uuid", "UPS", 4)
		Expect(asRead).NotTo(ContainSubstring(`"type":"SUPPLIER","target"`))

		file := tempFile(asRead)
		Expect(c.run("connection", "update", "c2", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.OK))

		put := api.calls[len(api.calls)-1]
		Expect(put.Method).To(Equal(http.MethodPut))
		Expect(put.json()).To(HaveKeyWithValue("type", typeSupplier))
	})

	It("reads the current version and sends it back", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connection("c2", typeSupplier, "sup-uuid", "UPS", 4))
		})

		file := tempFile(connectionSupplierExample)
		Expect(c.run("connection", "update", "c2", "--facility", "BER-01", "--file", file)).To(Equal(exitcode.OK))

		// A GET to learn the version, then the PUT that carries it.
		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[0].Method).To(Equal(http.MethodGet))

		put := api.calls[1]
		Expect(put.Method).To(Equal(http.MethodPut))
		Expect(put.json()).To(HaveKeyWithValue("version", BeNumerically("==", 4)))
	})

	It("skips the read when --if-version says what the version is", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connection("c2", typeSupplier, "sup-uuid", "UPS", 9))
		})

		file := tempFile(connectionSupplierExample)
		Expect(c.run("connection", "update", "c2", "--facility", "BER-01",
			"--file", file, "--if-version", "7")).To(Equal(exitcode.OK))

		// One request, not two: the read exists only to learn the version.
		Expect(api.only().Method).To(Equal(http.MethodPut))
		Expect(api.only().json()).To(HaveKeyWithValue("version", BeNumerically("==", 7)))
	})

	It("requires --file: there is no PATCH, so a partial update is not a thing", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " for an update with no body")
		})

		Expect(c.run("connection", "update", "c2", "--facility", "BER-01")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("--file is required"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft connection delete", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	// A UUID is not a question anybody can answer. The connection is read first so
	// that the prompt can name what it is about to remove — the same reasoning that
	// makes `fft facility delete` name the facility.
	It("names the connection by its target and carrier, not by its id", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connection("c2", typeSupplier, "FRA-02", "DHL_V2", 1))
		})
		c.answer("n")

		Expect(c.run("connection", "delete", "c2", "--facility", "BER-01")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("the SUPPLIER connection to FRA-02 via DHL_V2"))
		Expect(c.errOut()).To(ContainSubstring("Orders will stop being routed through it"))
	})

	It("does not delete when the answer is no", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connection("c2", typeSupplier, "FRA-02", "DHL_V2", 1))
		})
		c.answer("n")

		Expect(c.run("connection", "delete", "c2", "--facility", "BER-01")).To(Equal(exitcode.OK))

		// The read that phrased the question, and nothing else.
		Expect(api.only().Method).To(Equal(http.MethodGet))
		Expect(c.errOut()).To(ContainSubstring("was not deleted"))
	})

	It("deletes without asking, and without the read, under --yes", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.WriteHeader(http.StatusOK)
		})

		Expect(c.run("connection", "delete", "c2", "--facility", "BER-01", "--yes")).To(Equal(exitcode.OK))

		// The lookup exists only to phrase a question nobody is being asked.
		Expect(api.only().Method).To(Equal(http.MethodDelete))
		Expect(api.only().Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01/connections/c2"))
	})

	// A prompt nobody can see is not consent, and defaulting to yes is how a pipeline
	// removes a routing edge at 3am.
	It("refuses rather than assuming yes when there is nobody to ask", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, connection("c2", typeSupplier, "FRA-02", "DHL_V2", 1))
		})

		Expect(c.run("connection", "delete", "c2", "--facility", "BER-01")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("--yes"))
	})
})
