package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// fixture reads a recorded API entity from testdata.
func fixture(name string) string {
	GinkgoHelper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}

// searchPage builds the envelope POST /api/facilities/search answers with. total
// is omitted when it is nil — which is what the API does unless the search asked
// for it, and the distinction this suite has to be able to make.
func searchPage(facilities []string, hasNext bool, endCursor string, total *int) string {
	envelope := fmt.Sprintf(
		`{"facilities":[%s],"pageInfo":{"hasNextPage":%t,"endCursor":%q,"hasPreviousPage":false,"startCursor":"a"}`,
		strings.Join(facilities, ","), hasNext, endCursor)

	if total != nil {
		envelope += fmt.Sprintf(`,"total":%d`, *total)
	}
	return envelope + "}"
}

// writeJSON answers a request the way the tenant does.
func writeJSON(w http.ResponseWriter, status int, body string) {
	GinkgoHelper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err := w.Write([]byte(body))
	Expect(err).NotTo(HaveOccurred())
}

// tempFile writes a request body for --file to read.
func tempFile(content string) string {
	GinkgoHelper()

	path := filepath.Join(GinkgoT().TempDir(), "body.json")
	Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
	return path
}

var _ = Describe("fft facility list", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	When("the tenant has facilities of both kinds", func() {
		var api *tenant

		BeforeEach(func() {
			api = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, searchPage(
					[]string{fixture("facility_managed.json"), fixture("facility_supplier.json")},
					false, "", nil))
			})
		})

		It("renders a managed facility and a supplier in one table", func() {
			Expect(c.run("facility", "list")).To(Equal(exitcode.OK))

			// A supplier has no locationType and no address, and its cells are dashes
			// rather than blanks — the two kinds still line up under one header.
			Expect(c.out()).To(Equal(strings.Join([]string{
				"ID                                     TENANT ID    NAME                 TYPE               STATUS      LOCATION   CITY     VERSION",
				"8f14e45f-ceea-467a-9575-25a1b5c8b3a1   BER-01       Berlin Mitte         MANAGED_FACILITY   ONLINE      STORE      Berlin   41",
				"b1946ac9-2492-4ba0-9a6f-2f4f2b2a1f77   0090000042   Nordwaren Logistik   SUPPLIER           SUSPENDED   -          -        3",
				"",
			}, "\n")))
		})

		It("shows the platform id, which the generated model has no field for", func() {
			// api.Facility has no Id — the swagger omits it — so this column can only
			// come from fft's own view struct. If someone "simplifies" the view model
			// away and decodes into the generated type, this is the spec that says no.
			Expect(c.run("facility", "list")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("8f14e45f-ceea-467a-9575-25a1b5c8b3a1"))
		})

		It("searches on the cursor API, not the legacy GET list", func() {
			Expect(c.run("facility", "list")).To(Equal(exitcode.OK))

			Expect(api.only().Method).To(Equal(http.MethodPost))
			Expect(api.only().Path).To(Equal("/api/facilities/search"))
		})

		It("asks for everything when no filter is given", func() {
			Expect(c.run("facility", "list")).To(Equal(exitcode.OK))

			// {"query":{}} is what the live tenant accepts as "all of them".
			Expect(api.only().json()).To(HaveKeyWithValue("query", BeEmpty()))
		})

		It("emits the API's own JSON on stdout and leaves stderr clean with -o json", func() {
			Expect(c.run("facility", "list", "-o", "json")).To(Equal(exitcode.OK))

			var facilities []map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &facilities)).To(Succeed())
			Expect(facilities).To(HaveLen(2))

			// Full fidelity: the fields fft's table never shows are still there for jq.
			Expect(facilities[0]).To(HaveKeyWithValue("fulfillmentProcessBuffer", BeNumerically("==", 240)))
			Expect(facilities[0]).To(HaveKey("created"))
			Expect(facilities[0]).To(HaveKey("services"))

			Expect(c.errOut()).To(BeEmpty())
		})
	})

	When("the search asks for the total", func() {
		It("reports it on stderr, keeping stdout to the data", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, searchPage(
					[]string{fixture("facility_managed.json")}, false, "", ptr(2133)))
			})

			Expect(c.run("facility", "list", "--total", "-o", "json")).To(Equal(exitcode.OK))

			Expect(api.only().json()).To(HaveKeyWithValue("options",
				HaveKeyWithValue("withTotal", true)))

			Expect(c.errOut()).To(ContainSubstring("Total: 2133"))
			Expect(c.out()).NotTo(ContainSubstring("2133"))
			Expect(json.Valid([]byte(c.out()))).To(BeTrue())
		})

		It("reports a total of zero as zero", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, searchPage(nil, false, "", ptr(0)))
			})

			Expect(c.run("facility", "list", "--total")).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("Total: 0"))
		})
	})

	When("--all and --total are given together", func() {
		It("reports the count it actually fetched, and does not make the API count too", func() {
			// Regression: --total used to set options.withTotal on every page of the
			// cursor — paying the server to count, once per page — and then throw the
			// answer away, because SearchAll yields items and not the envelope. The
			// command printed no total at all.
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
				var payload struct {
					After *string `json:"after"`
				}
				Expect(json.Unmarshal(body, &payload)).To(Succeed())

				if payload.After == nil {
					writeJSON(w, http.StatusOK, searchPage(
						[]string{fixture("facility_managed.json")}, true, "cursor-2", nil))
					return
				}
				writeJSON(w, http.StatusOK, searchPage(
					[]string{fixture("facility_supplier.json")}, false, "", nil))
			})

			Expect(c.run("facility", "list", "--all", "--total")).To(Equal(exitcode.OK))

			// --all read every match, so the count *is* the total.
			Expect(c.errOut()).To(ContainSubstring("Total: 2"))

			// And no page asked the API to count: the answer was already in hand.
			for _, sent := range api.calls {
				Expect(sent.json()).NotTo(HaveKey("options"))
			}
		})

		It("says nothing about a total it could not know, when it stopped short", func() {
			// A cursor that always advances and never ends: without a cap this would
			// page forever. (It has to *advance* — SearchAll treats a repeated cursor
			// as a loop and refuses it, which is a different behaviour than this spec
			// is about.)
			page := 0
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				page++
				writeJSON(w, http.StatusOK, searchPage(
					[]string{fixture("facility_managed.json")}, true, fmt.Sprintf("cursor-%d", page), nil))
			})

			Expect(c.run("facility", "list", "--all", "--total", "--max-items", "2")).To(Equal(exitcode.OK))

			// A truncated run knows how many it fetched and nothing about how many it
			// did not. Reporting the former as the latter would be a wrong number
			// stated with confidence.
			Expect(c.errOut()).To(ContainSubstring("there are more results"))
			Expect(c.errOut()).NotTo(ContainSubstring("Total:"))
		})
	})

	When("the search did not ask for the total", func() {
		It("says nothing about it, because absent is not zero", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, searchPage(
					[]string{fixture("facility_managed.json")}, false, "", nil))
			})

			Expect(c.run("facility", "list")).To(Equal(exitcode.OK))

			// A "Total: 0" here would be fft inventing a number the API never sent.
			Expect(c.errOut()).NotTo(ContainSubstring("Total"))
		})
	})

	When("nothing matches", func() {
		BeforeEach(func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, searchPage(nil, false, "", nil))
			})
		})

		It("leaves stdout empty and says so on stderr", func() {
			Expect(c.run("facility", "list")).To(Equal(exitcode.OK))

			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("No facilities found."))
		})

		It("still emits a parseable empty array under -o json", func() {
			Expect(c.run("facility", "list", "-o", "json")).To(Equal(exitcode.OK))

			// `| jq length` has to answer 0, not fail.
			Expect(strings.TrimSpace(c.out())).To(Equal("[]"))
		})
	})

	When("--all is given", func() {
		It("follows the cursor to the end", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, body []byte) {
				var payload struct {
					After *string `json:"after"`
				}
				Expect(json.Unmarshal(body, &payload)).To(Succeed())

				if payload.After == nil {
					writeJSON(w, http.StatusOK, searchPage(
						[]string{fixture("facility_managed.json")}, true, "cursor-2", nil))
					return
				}
				Expect(*payload.After).To(Equal("cursor-2"))
				writeJSON(w, http.StatusOK, searchPage(
					[]string{fixture("facility_supplier.json")}, false, "", nil))
			})

			Expect(c.run("facility", "list", "--all", "-o", "json")).To(Equal(exitcode.OK))

			Expect(api.calls).To(HaveLen(2))

			var facilities []map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &facilities)).To(Succeed())
			Expect(facilities).To(HaveLen(2))
		})

		It("warns on stderr — never on stdout — when it stops short of the end", func() {
			// A cursor that never ends: without a cap, this would page forever.
			page := 0
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				page++
				writeJSON(w, http.StatusOK, searchPage(
					[]string{fixture("facility_managed.json"), fixture("facility_supplier.json")},
					true, fmt.Sprintf("cursor-%d", page), nil))
			})

			Expect(c.run("facility", "list", "--all", "--max-items", "3", "-o", "json")).To(Equal(exitcode.OK))

			// The truncation is announced, loudly, and only to the human.
			Expect(c.errOut()).To(ContainSubstring("there are more results"))
			Expect(c.errOut()).To(ContainSubstring("--max-items"))

			// A truncated list that does not say so is a wrong answer that looks right —
			// but the notice must not end up in the pipe either.
			var facilities []map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &facilities)).To(Succeed())
			Expect(facilities).To(HaveLen(3))
			Expect(c.out()).NotTo(ContainSubstring("more results"))
		})
	})

	Describe("the filters", func() {
		var api *tenant

		BeforeEach(func() {
			api = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, searchPage(
					[]string{fixture("facility_managed.json")}, false, "", nil))
			})
		})

		It("sends --status as the enum 'in' filter the API's schema declares", func() {
			Expect(c.run("facility", "list", "--status", "ONLINE,SUSPENDED")).To(Equal(exitcode.OK))

			Expect(api.only().json()).To(HaveKeyWithValue("query",
				HaveKeyWithValue("status", HaveKeyWithValue("in", ConsistOf("ONLINE", "SUSPENDED")))))
		})

		It("accepts a status in the case a human types it", func() {
			Expect(c.run("facility", "list", "--status", "online")).To(Equal(exitcode.OK))

			Expect(api.only().json()).To(HaveKeyWithValue("query",
				HaveKeyWithValue("status", HaveKeyWithValue("in", ConsistOf("ONLINE")))))
		})

		It("sends --type as an 'eq' filter", func() {
			Expect(c.run("facility", "list", "--type", "SUPPLIER")).To(Equal(exitcode.OK))

			Expect(api.only().json()).To(HaveKeyWithValue("query",
				HaveKeyWithValue("type", HaveKeyWithValue("eq", "SUPPLIER"))))
		})

		It("sends --tenant-facility-id as an 'eq' filter, not as a URN", func() {
			// The URN wrap belongs to path parameters. In a query it would match nothing.
			Expect(c.run("facility", "list", "--tenant-facility-id", "BER-01")).To(Equal(exitcode.OK))

			Expect(api.only().json()).To(HaveKeyWithValue("query",
				HaveKeyWithValue("tenantFacilityId", HaveKeyWithValue("eq", "BER-01"))))
		})

		It("sends --sort as the one-element struct the API requires", func() {
			Expect(c.run("facility", "list", "--sort", "name:desc")).To(Equal(exitcode.OK))

			Expect(api.only().json()).To(HaveKeyWithValue("sort",
				ConsistOf(HaveKeyWithValue("name", "DESC"))))
		})

		DescribeTable("refusing a value the API would reject with an opaque 400",
			func(args ...string) {
				Expect(c.run(append([]string{"facility", "list"}, args...)...)).To(Equal(exitcode.Usage))
				Expect(api.calls).To(BeEmpty(), "the request should never have been sent")
			},
			Entry("an unknown status", "--status", "PAUSED"),
			Entry("an unknown type", "--type", "DARKSTORE"),
			Entry("a sort with no direction", "--sort", "name"),
			Entry("a sort in a direction that is not one", "--sort", "name:sideways"),
			Entry("a sort by a field the API cannot sort on", "--sort", "city:asc"),
			Entry("a page size above the API's maximum", "--size", "500"),

			// Regression: --size 0 and --size -1 used to be silently ignored, quietly
			// giving the user the API's default of 20 while --size 500 was refused.
			// Both ends of the range must fail the same way.
			Entry("a page size of zero", "--size", "0"),
			Entry("a negative page size", "--size", "-1"),
		)
	})
})

var _ = Describe("fft facility list -o yaml", func() {
	It("renders the API's own document as YAML, with numbers still numbers", func() {
		c := newCLI()
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, searchPage(
				[]string{fixture("facility_managed.json")}, false, "", nil))
		})

		Expect(c.run("facility", "list", "-o", "yaml")).To(Equal(exitcode.OK))

		// `version: 41`, never `version: "41"` — the round trip through json.Number
		// must not turn the API's numbers into strings.
		Expect(c.out()).To(ContainSubstring("version: 41"))
		Expect(c.out()).To(ContainSubstring("tenantFacilityId: BER-01"))
		Expect(c.errOut()).To(BeEmpty())
	})
})

var _ = Describe("fft facility get", func() {
	var (
		c   *cli
		api *tenant
	)

	BeforeEach(func() {
		c = newCLI()
		api = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("facility_supplier.json"))
		})
	})

	It("wraps a tenantFacilityId in the URN every facility endpoint accepts", func() {
		Expect(c.run("facility", "get", "0090000042")).To(Equal(exitcode.OK))

		// The tenant's own ids are numeric strings — they look nothing like a UUID —
		// so this wrap is what makes `fft facility get 0090000042` work at all.
		Expect(api.only().Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:0090000042"))
	})

	It("passes a platform UUID through untouched", func() {
		Expect(c.run("facility", "get", "b1946ac9-2492-4ba0-9a6f-2f4f2b2a1f77")).To(Equal(exitcode.OK))

		Expect(api.only().Path).To(Equal("/api/facilities/b1946ac9-2492-4ba0-9a6f-2f4f2b2a1f77"))
	})

	It("prints the API's own object — not an array of one — under -o json", func() {
		Expect(c.run("facility", "get", "0090000042", "-o", "json")).To(Equal(exitcode.OK))

		// `fft facility get x -o json | jq .name` must work without indexing a list.
		var facility map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &facility)).To(Succeed())
		Expect(facility).To(HaveKeyWithValue("name", "Nordwaren Logistik"))
		Expect(c.errOut()).To(BeEmpty())
	})

	When("the facility does not exist", func() {
		BeforeEach(func() {
			api = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusNotFound, `[{"summary":"no facility matching request was found"}]`)
			})
		})

		It("exits 6 and repeats what the API said", func() {
			Expect(c.run("facility", "get", "nope")).To(Equal(exitcode.NotFound))

			Expect(c.errOut()).To(ContainSubstring("no facility matching request was found"))
			Expect(c.out()).To(BeEmpty())
		})
	})
})

var _ = Describe("fft facility create", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("prints an example body without a project, a credential or a request", func() {
		// A user reaching for --example usually has not set fft up yet, so it must
		// not need any of that.
		Expect(c.run("facility", "create", "--example")).To(Equal(exitcode.OK))

		var body map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &body)).To(Succeed())
		Expect(body).To(HaveKeyWithValue("type", "MANAGED_FACILITY"))
		Expect(body).To(HaveKey("name"))
		Expect(body).To(HaveKey("address"))
		Expect(c.errOut()).To(BeEmpty())
	})

	It("posts the file and renders what came back", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, fixture("facility_managed.json"))
		})

		file := tempFile(`{"type":"MANAGED_FACILITY","name":"Berlin Mitte","address":{"city":"Berlin"}}`)

		Expect(c.run("facility", "create", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/facilities"))
		Expect(api.only().json()).To(HaveKeyWithValue("name", "Berlin Mitte"))

		Expect(c.out()).To(ContainSubstring("Berlin Mitte"))
	})

	It("refuses a body with no type, naming the two that exist", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		file := tempFile(`{"name":"Berlin Mitte"}`)

		Expect(c.run("facility", "create", "--file", file)).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("MANAGED_FACILITY"))
		Expect(c.errOut()).To(ContainSubstring("SUPPLIER"))
		Expect(api.calls).To(BeEmpty())
	})

	It("refuses a file that is not JSON", func() {
		c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("facility", "create", "--file", tempFile("not json"))).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("valid JSON"))
	})

	It("reads the body from stdin when --file is -", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, fixture("facility_managed.json"))
		})

		_, err := c.stdin.WriteString(`{"type":"SUPPLIER","name":"Piped In"}`)
		Expect(err).NotTo(HaveOccurred())

		Expect(c.run("facility", "create", "--file", "-")).To(Equal(exitcode.OK))

		Expect(api.only().json()).To(HaveKeyWithValue("name", "Piped In"))
	})

	It("names the created facility by the id the API gave it, not by the name it was sent", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, fixture("facility_managed.json"))
		})

		file := tempFile(`{"type":"MANAGED_FACILITY","name":"Berlin Mitte"}`)

		Expect(c.run("facility", "create", "--file", file)).To(Equal(exitcode.OK))

		// The platform id is the one thing the user did not already know, and the
		// thing the next command needs.
		Expect(c.errOut()).To(ContainSubstring("Created facility 8f14e45f-ceea-467a-9575-25a1b5c8b3a1."))
	})

	It("never sends the create twice, whatever the API answers", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusInternalServerError, `[{"summary":"the server is having a moment"}]`)
		})

		file := tempFile(`{"type":"MANAGED_FACILITY","name":"Berlin Mitte"}`)

		Expect(c.run("facility", "create", "--file", file)).To(Equal(exitcode.Unavailable))

		// A 500 on a POST does not mean the facility was not created — it means fft
		// was not told. Retrying would risk a second facility, so it does not.
		Expect(api.calls).To(HaveLen(1))
	})
})

var _ = Describe("fft facility list --debug", func() {
	It("traces to stderr, leaving stdout as clean JSON", func() {
		c := newCLI()
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, searchPage(
				[]string{fixture("facility_managed.json")}, false, "", nil))
		})

		Expect(c.run("facility", "list", "--debug", "-o", "json")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("POST"))
		Expect(c.errOut()).To(ContainSubstring("/api/facilities/search"))

		// The trace names the header but never its value: httplog redacts the bearer
		// token, and stderr is where a leak would actually have shown up.
		Expect(c.errOut()).To(ContainSubstring("Authorization"))
		Expect(c.errOut()).NotTo(ContainSubstring(testIDToken))

		// `fft facility list -o json --debug | jq` must still be piping JSON and
		// nothing else.
		Expect(c.out()).NotTo(ContainSubstring("Authorization"))

		var facilities []map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &facilities)).To(Succeed())
	})
})

var _ = Describe("fft facility update", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("reads the facility for its version, then replaces it with the file", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
		})

		file := tempFile(`{"type":"MANAGED_FACILITY","name":"Berlin Neu","address":{"city":"Berlin"}}`)

		Expect(c.run("facility", "update", "BER-01", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[0].Method).To(Equal(http.MethodGet))

		put := api.calls[1]
		Expect(put.Method).To(Equal(http.MethodPut))
		Expect(put.Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01"))
		Expect(put.json()).To(HaveKeyWithValue("name", "Berlin Neu"))

		// The version came from the read; the user's file never carried one.
		Expect(put.json()).To(HaveKeyWithValue("version", BeNumerically("==", 41)))
	})

	It("skips the read when --if-version says what the version is", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
		})

		file := tempFile(`{"type":"MANAGED_FACILITY","name":"Berlin Neu","address":{"city":"Berlin"}}`)

		Expect(c.run("facility", "update", "BER-01", "--file", file, "--if-version", "41")).To(Equal(exitcode.OK))

		// One request, not two: that is the whole point of the flag.
		Expect(api.only().Method).To(Equal(http.MethodPut))
		Expect(api.only().json()).To(HaveKeyWithValue("version", BeNumerically("==", 41)))
	})

	It("exits 7 on a stale --if-version, and says which version to send", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusConflict,
				`[{"summary":"version conflict","version":42,"requestVersion":41}]`)
		})

		file := tempFile(`{"type":"MANAGED_FACILITY","name":"Berlin Neu"}`)

		Expect(c.run("facility", "update", "BER-01", "--file", file, "--if-version", "41")).
			To(Equal(exitcode.Conflict))

		Expect(c.errOut()).To(ContainSubstring("you sent v41, current is v42"))
		Expect(c.errOut()).To(ContainSubstring("--if-version 42"))
		Expect(c.out()).To(BeEmpty())
	})
})

var _ = Describe("fft facility patch", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("reads the facility, sends only the changed fields, and carries the version it read", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
		})

		Expect(c.run("facility", "patch", "BER-01", "--name", "Berlin Neu")).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))

		patch := api.calls[1]
		Expect(patch.Method).To(Equal(http.MethodPatch))
		Expect(patch.json()).To(HaveKeyWithValue("name", "Berlin Neu"))
		Expect(patch.json()).To(HaveKeyWithValue("version", BeNumerically("==", 41)))

		// The type is the PATCH union's discriminator: without it the API cannot tell
		// which of the two shapes the body is.
		Expect(patch.json()).To(HaveKeyWithValue("type", "MANAGED_FACILITY"))

		// A patch is not a replace: the fields fft did not touch are not sent back,
		// and so cannot be clobbered.
		Expect(patch.json()).NotTo(HaveKey("address"))
		Expect(patch.json()).NotTo(HaveKey("services"))
	})

	It("re-reads and retries exactly once when someone wrote in between", func() {
		var patches int

		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.Method == http.MethodGet {
				// The second read sees the version the other writer left behind.
				version := 41
				if patches > 0 {
					version = 42
				}
				writeJSON(w, http.StatusOK, fmt.Sprintf(
					`{"id":"x","type":"MANAGED_FACILITY","name":"Berlin Mitte","version":%d}`, version))
				return
			}

			patches++
			if patches == 1 {
				writeJSON(w, http.StatusConflict,
					`[{"summary":"version conflict","version":42,"requestVersion":41}]`)
				return
			}
			writeJSON(w, http.StatusOK, `{"id":"x","type":"MANAGED_FACILITY","name":"Berlin Neu","version":43}`)
		})

		Expect(c.run("facility", "patch", "BER-01", "--name", "Berlin Neu")).To(Equal(exitcode.OK))

		// GET, PATCH(409), GET, PATCH(200) — the mutation is re-applied to the fresh
		// facility, not replayed against the stale one.
		Expect(api.calls).To(HaveLen(4))
		Expect(api.calls[3].json()).To(HaveKeyWithValue("version", BeNumerically("==", 42)))
		Expect(api.calls[3].json()).To(HaveKeyWithValue("name", "Berlin Neu"))
	})

	It("gives up after a second conflict rather than looping", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
				return
			}
			writeJSON(w, http.StatusConflict,
				`[{"summary":"version conflict","version":99,"requestVersion":41}]`)
		})

		Expect(c.run("facility", "patch", "BER-01", "--name", "Berlin Neu")).To(Equal(exitcode.Conflict))

		// Two attempts, and then the truth: something is writing faster than fft can
		// read, and trying a third time would only take longer to say so.
		Expect(api.calls).To(HaveLen(4))
		Expect(c.errOut()).To(ContainSubstring("you sent v41, current is v99"))
	})

	It("skips the read when --if-version and --type are both given", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
		})

		Expect(c.run("facility", "patch", "BER-01",
			"--name", "Berlin Neu", "--type", "MANAGED_FACILITY", "--if-version", "41")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPatch))
		Expect(api.only().json()).To(HaveKeyWithValue("version", BeNumerically("==", 41)))
		Expect(api.only().json()).To(HaveKeyWithValue("type", "MANAGED_FACILITY"))
	})

	It("refuses --if-version without --type, because the read is where the type came from", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		Expect(c.run("facility", "patch", "BER-01", "--name", "x", "--if-version", "41")).
			To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--type"))
		Expect(api.calls).To(BeEmpty())
	})

	It("refuses a --type that contradicts the facility it just read", func() {
		// Regression: --type used to overwrite the type read from the API, sending a
		// discriminator that disagreed with the entity it addressed. A facility's type
		// is fixed at creation, so this is always a mistake worth naming.
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
		})

		Expect(c.run("facility", "patch", "BER-01", "--name", "x", "--type", "SUPPLIER")).
			To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("is a MANAGED_FACILITY, not a SUPPLIER"))

		// The read happened; the write did not.
		Expect(api.calls).To(HaveLen(1))
		Expect(api.calls[0].Method).To(Equal(http.MethodGet))
	})

	It("succeeds when the API answers the patch with no body at all", func() {
		// A 2xx with an empty body used to fail the command *after* the write had
		// landed — and the user's natural response to that is to run the mutation a
		// second time.
		c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

		Expect(c.run("facility", "patch", "BER-01", "--name", "Berlin Neu")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("Patched facility"))
		Expect(c.out()).To(BeEmpty())
	})

	It("refuses a patch that would change nothing", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		// The API would answer 200 and change nothing, which looks exactly like
		// success and is how a broken script goes unnoticed for a week.
		Expect(c.run("facility", "patch", "BER-01")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("nothing to patch"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft facility delete", func() {
	var (
		c   *cli
		api *tenant
	)

	BeforeEach(func() {
		c = newCLI()
		api = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.WriteHeader(http.StatusNoContent)
		})
	})

	It("refuses without --yes when there is no terminal to ask on", func() {
		// A prompt nobody can see is not consent. Defaulting to yes here is how a
		// pipeline deletes a production facility at 3am.
		Expect(c.run("facility", "delete", "BER-01")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--yes"))
		Expect(api.calls).To(BeEmpty(), "nothing may be deleted without an answer")
	})

	It("deletes with --yes, and says so on stderr rather than in the pipe", func() {
		Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodDelete))
		Expect(api.only().Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01"))

		Expect(c.out()).To(BeEmpty())
		Expect(c.errOut()).To(ContainSubstring("Deleted facility"))
	})
})

var _ = Describe("fft facility coordinates", func() {
	var (
		c   *cli
		api *tenant
	)

	BeforeEach(func() {
		c = newCLI()
		api = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
		})
	})

	It("posts UPDATE_FACILITY_COORDINATES with the version it read", func() {
		Expect(c.run("facility", "coordinates", "set", "BER-01", "--lat", "52.5219", "--lon", "13.4132")).
			To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))

		action := api.calls[1]
		Expect(action.Method).To(Equal(http.MethodPost))
		Expect(action.Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01/actions"))

		// The generated model for this action has no coordinates field at all — the
		// swagger's allOf collapses to a bare alias — so the body is hand-built, and
		// this is the spec that guards it.
		Expect(action.json()).To(HaveKeyWithValue("name", "UPDATE_FACILITY_COORDINATES"))
		Expect(action.json()).To(HaveKeyWithValue("version", BeNumerically("==", 41)))
		Expect(action.json()).To(HaveKeyWithValue("coordinates", SatisfyAll(
			HaveKeyWithValue("lat", BeNumerically("~", 52.5219, 0.0001)),
			HaveKeyWithValue("lon", BeNumerically("~", 13.4132, 0.0001)),
		)))
	})

	It("posts REMOVE_FACILITY_COORDINATES, which carries no coordinates", func() {
		Expect(c.run("facility", "coordinates", "remove", "BER-01")).To(Equal(exitcode.OK))

		action := api.calls[1]
		Expect(action.json()).To(HaveKeyWithValue("name", "REMOVE_FACILITY_COORDINATES"))
		Expect(action.json()).NotTo(HaveKey("coordinates"))
	})

	It("skips the read when --if-version is given", func() {
		Expect(c.run("facility", "coordinates", "remove", "BER-01", "--if-version", "41")).
			To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().json()).To(HaveKeyWithValue("version", BeNumerically("==", 41)))
	})

	DescribeTable("refusing a point that is not on the planet",
		func(lat, lon string) {
			Expect(c.run("facility", "coordinates", "set", "BER-01", "--lat", lat, "--lon", lon)).
				To(Equal(exitcode.Usage))
			Expect(api.calls).To(BeEmpty())
		},
		Entry("a latitude past the pole", "91", "13.4"),
		Entry("a longitude past the date line", "52.5", "181"),

		// Regression: pflag parses these, and every comparison against NaN is false,
		// so a bare range check waved them through — to die inside json.Marshal as an
		// unclassified exit-1 "unsupported value".
		Entry("a latitude that is not a number", "NaN", "13.4"),
		Entry("a longitude at infinity", "52.5", "Inf"),
	)

	It("refuses a set with no coordinates at all", func() {
		Expect(c.run("facility", "coordinates", "set", "BER-01")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("--lat"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft facility search", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("sends the query in the file", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, searchPage(
				[]string{fixture("facility_managed.json")}, false, "", nil))
		})

		file := tempFile(`{"query":{"name":{"like":"Berlin.*"}},"sort":[{"name":"ASC"}]}`)

		Expect(c.run("facility", "search", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Path).To(Equal("/api/facilities/search"))
		Expect(api.only().json()).To(HaveKeyWithValue("query",
			HaveKeyWithValue("name", HaveKeyWithValue("like", "Berlin.*"))))
	})

	It("names the misspelled field rather than letting the API silently not filter", func() {
		api := c.fakeTenant(func(http.ResponseWriter, *http.Request, []byte) {})

		// The API answers `{"statuz": …}` with a 200 listing *everything* — a filter
		// that silently does not filter. Checking the body against the schema first
		// turns that into a sentence.
		file := tempFile(`{"query":{"statuz":{"eq":"ONLINE"}}}`)

		Expect(c.run("facility", "search", "--file", file)).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring("statuz"))
		Expect(api.calls).To(BeEmpty())
	})
})
