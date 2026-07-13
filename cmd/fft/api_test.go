package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("fft api", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	// answers is a tenant that returns body to every request.
	answers := func(body string) *tenant {
		return c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(body))
			Expect(err).NotTo(HaveOccurred())
		})
	}

	Describe("calling an operation the typed client does not have", func() {
		It("reaches an endpoint that is in no generated method at all", func() {
			// queryPickJobs is not one of the five tags oapi-codegen was pointed at, so
			// there is no Go method for it anywhere. This is the whole point of the tier.
			t := answers(`{"pickJobs":[{"id":"pj-1"}],"total":1}`)

			Expect(c.run("api", "queryPickJobs")).To(Equal(exitcode.OK))

			Expect(t.only().Method).To(Equal(http.MethodGet))
			Expect(t.only().Path).To(Equal("/api/pickjobs"))
			Expect(c.out()).To(ContainSubstring("pj-1"))
		})

		It("fills a path parameter from --param", func() {
			t := answers(`{"id":"pj-1"}`)

			Expect(c.run("api", "getPickJob", "--param", "pickJobId=pj-1")).To(Equal(exitcode.OK))

			Expect(t.only().Path).To(Equal("/api/pickjobs/pj-1"))
		})

		It("prints the API's answer as the API sent it", func() {
			answers(`{"id":"pj-1","combinedId":"a field fft has no model for"}`)

			Expect(c.run("api", "getPickJob", "--param", "pickJobId=pj-1", "-o", "json")).To(Equal(exitcode.OK))

			var doc map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &doc)).To(Succeed())
			Expect(doc).To(HaveKeyWithValue("combinedId", "a field fft has no model for"))
		})

		It("says so on stderr when the API answers with no content, and leaves stdout empty", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				w.WriteHeader(http.StatusNoContent)
			})

			Expect(c.run("api", "deleteCategory", "--param", "categoryId=c-1")).To(Equal(exitcode.OK))

			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("204"))
		})
	})

	// The bug class the tier is built around: the API answers 200 to either encoding,
	// so the wrong one does not fail — it filters on something else and reports no
	// results, which the user believes.
	//
	// getStowJobs and getServiceJobs are the demonstration: both take a query
	// parameter called `status`, both are arrays of an enum — and the spec says one is
	// comma-joined and the other is repeated. There is no global policy that serves
	// both, and the API accepts either form from either endpoint and answers 200.
	Describe("the explode encoding of an array query parameter", func() {
		It("comma-joins a parameter the spec marks explode:false", func() {
			t := answers(`{"stowJobs":[]}`)

			Expect(c.run("api", "getStowJobs",
				"--query", "status=OPEN", "--query", "status=IN_PROGRESS")).To(Equal(exitcode.OK))

			Expect(t.only().RawQuery).To(Equal("status=OPEN%2CIN_PROGRESS"))
		})

		It("repeats the name of a parameter the spec marks explode:true", func() {
			t := answers(`{"serviceJobs":[]}`)

			Expect(c.run("api", "getServiceJobs",
				"--query", "status=OPEN", "--query", "status=IN_PROGRESS")).To(Equal(exitcode.OK))

			Expect(t.only().RawQuery).To(Equal("status=OPEN&status=IN_PROGRESS"))
		})

		It("reads a comma-separated value as the list it is, under either encoding", func() {
			// So that --query status=OPEN,IN_PROGRESS means the same two values whichever
			// endpoint it is sent to, and comes out correctly encoded for that one.
			joined := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{"stowJobs":[]}`))
				Expect(err).NotTo(HaveOccurred())
			})

			Expect(c.run("api", "getStowJobs", "--query", "status=OPEN,IN_PROGRESS")).To(Equal(exitcode.OK))
			Expect(c.run("api", "getServiceJobs", "--query", "status=OPEN,IN_PROGRESS")).To(Equal(exitcode.OK))

			Expect(joined.calls).To(HaveLen(2))
			Expect(joined.calls[0].RawQuery).To(Equal("status=OPEN%2CIN_PROGRESS"))
			Expect(joined.calls[1].RawQuery).To(Equal("status=OPEN&status=IN_PROGRESS"))
		})

		It("refuses an enum value the API does not have, rather than filtering on nothing", func() {
			t := answers(`{"stowJobs":[]}`)

			code := c.run("api", "getStowJobs", "--query", "status=OPNE")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("unknown status"))
			Expect(c.errOut()).To(ContainSubstring("OPEN"))
			Expect(t.calls).To(BeEmpty(), "a request was sent with a status the API does not have")
		})

		It("forgives the case a human types", func() {
			t := answers(`{"stowJobs":[]}`)

			Expect(c.run("api", "getStowJobs", "--query", "status=open")).To(Equal(exitcode.OK))

			Expect(t.only().RawQuery).To(Equal("status=OPEN"))
		})

		// Regression: an empty value used to be split into no values at all, and a
		// parameter with no values is not sent. The API then answered 200 with EVERY
		// row, and the user read an unfiltered list as a filtered one — the same
		// silent-wrong-filter class as the encoding itself, but in the direction that is
		// harder to notice. `--query status=` is what a script writes when its $STATUS
		// is unset.
		It("refuses an empty list value rather than silently sending no filter", func() {
			t := answers(`{"stowJobs":[{"id":"everything"}]}`)

			code := c.run("api", "getStowJobs", "--query", "status=")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("no value"))
			Expect(t.calls).To(BeEmpty(), "an unfiltered request was sent for an empty filter")
		})

		It("refuses an empty list value given as a flag on a generated command", func() {
			t := answers(`{"stowJobs":[{"id":"everything"}]}`)

			code := c.run("stowing", "get-stow-jobs", "--status", "")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(t.calls).To(BeEmpty())
		})
	})

	Describe("an operationId that does not exist", func() {
		It("exits 2 and suggests the one that was meant", func() {
			t := answers(`{}`)

			code := c.run("api", "getPickjob", "--param", "pickJobId=x")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("there is no operation"))
			Expect(c.errOut()).To(ContainSubstring("Did you mean"))
			Expect(c.errOut()).To(ContainSubstring("getPickJob"))
			Expect(t.calls).To(BeEmpty())
		})

		It("points at 'fft api list' when nothing is close enough to suggest", func() {
			code := c.run("api", "zzzzqqqqxxxx")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("fft api list"))
		})
	})

	Describe("a missing required parameter", func() {
		It("exits 2 and sends nothing at all", func() {
			// The check is worth its own spec because it has to happen *before* the
			// client is built: the user should not pay a sign-in and a round trip for a
			// request fft could see was incomplete.
			t := answers(`{}`)

			code := c.run("api", "getPickJob")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("pickJobId"))
			Expect(t.calls).To(BeEmpty(), "the tenant was sent a request although a required parameter was missing")
		})

		It("exits 2 when a required body is missing, and names --example", func() {
			t := answers(`{}`)

			code := c.run("api", "addPickJob")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("--example"))
			Expect(t.calls).To(BeEmpty())
		})

		It("refuses a path parameter the operation does not have", func() {
			t := answers(`{}`)

			code := c.run("api", "queryPickJobs", "--param", "pickJobId=x")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("no path parameter"))
			Expect(t.calls).To(BeEmpty())
		})

		It("refuses a body for an operation that takes none", func() {
			t := answers(`{}`)

			code := c.run("api", "getPickJob", "--param", "pickJobId=x", "--data", `{"a":1}`)

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("takes no request body"))
			Expect(t.calls).To(BeEmpty())
		})
	})

	Describe("an undeclared query parameter", func() {
		It("warns on stderr and sends it anyway", func() {
			// `fft api` is the escape hatch. One that refused a parameter the spec forgot
			// would not be one — but a filter that silently vanishes is the bug this whole
			// milestone is about, so the user is told.
			t := answers(`{"pickJobs":[]}`)

			Expect(c.run("api", "queryPickJobs", "--query", "statuz=OPEN")).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring(`no query parameter "statuz"`))
			Expect(t.only().RawQuery).To(Equal("statuz=OPEN"))
		})

		It("keeps the warning off stdout, so a pipe into jq is not contaminated", func() {
			answers(`{"pickJobs":[]}`)

			Expect(c.run("api", "queryPickJobs", "--query", "statuz=OPEN", "-o", "json")).To(Equal(exitcode.OK))

			Expect(json.Valid([]byte(c.out()))).To(BeTrue(), "stdout was not valid JSON: %s", c.out())
		})
	})

	Describe("the request body", func() {
		It("takes it inline from --data", func() {
			t := answers(`{"id":"pj-1"}`)

			Expect(c.run("api", "addPickJob", "--data", `{"tenantOrderId":"R1"}`)).To(Equal(exitcode.OK))

			Expect(t.only().json()).To(HaveKeyWithValue("tenantOrderId", "R1"))
		})

		It("takes it from a file with --data @file", func() {
			path := filepath.Join(GinkgoT().TempDir(), "body.json")
			Expect(os.WriteFile(path, []byte(`{"tenantOrderId":"R2"}`), 0o600)).To(Succeed())

			t := answers(`{"id":"pj-2"}`)

			Expect(c.run("api", "addPickJob", "--data", "@"+path)).To(Equal(exitcode.OK))

			Expect(t.only().json()).To(HaveKeyWithValue("tenantOrderId", "R2"))
		})

		It("takes it from a file with --file", func() {
			path := filepath.Join(GinkgoT().TempDir(), "body.json")
			Expect(os.WriteFile(path, []byte(`{"tenantOrderId":"R3"}`), 0o600)).To(Succeed())

			t := answers(`{"id":"pj-3"}`)

			Expect(c.run("api", "addPickJob", "--file", path)).To(Equal(exitcode.OK))

			Expect(t.only().json()).To(HaveKeyWithValue("tenantOrderId", "R3"))
		})

		It("reads stdin for --data -", func() {
			_, err := c.stdin.WriteString(`{"tenantOrderId":"R4"}`)
			Expect(err).NotTo(HaveOccurred())

			t := answers(`{"id":"pj-4"}`)

			Expect(c.run("api", "addPickJob", "--data", "-")).To(Equal(exitcode.OK))

			Expect(t.only().json()).To(HaveKeyWithValue("tenantOrderId", "R4"))
		})

		It("refuses a --data that is neither JSON nor a file", func() {
			t := answers(`{}`)

			code := c.run("api", "addPickJob", "--data", "not json")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(t.calls).To(BeEmpty())
		})
	})

	Describe("--example", func() {
		It("prints a valid JSON body to stdout and exits 0", func() {
			t := answers(`{}`)

			Expect(c.run("api", "addPickJob", "--example")).To(Equal(exitcode.OK))

			var body map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &body)).To(Succeed())
			Expect(body).NotTo(BeEmpty())
			Expect(t.calls).To(BeEmpty(), "--example sent a request")
		})

		It("needs no project and no credentials", func() {
			// Nothing is configured in this spec: no FFT_* variables, no config file. A
			// user reaching for --example usually has neither yet, and being asked to
			// sign in first would be absurd.
			Expect(c.run("api", "addPickJob", "--example")).To(Equal(exitcode.OK))

			Expect(json.Valid([]byte(c.out()))).To(BeTrue())
		})

		It("exits 2 when the operation takes no body at all", func() {
			// Asking a GET for its request body is a usage mistake, not a failure, and a
			// script telling the two apart by exit code has to be told the truth.
			code := c.run("api", "getPickJob", "--example")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("takes no request body"))
			Expect(c.out()).To(BeEmpty())
		})

		It("round-trips: the body it prints is one the API accepts back", func() {
			Expect(c.run("api", "addPickJob", "--example")).To(Equal(exitcode.OK))
			printed := c.out()

			t := answers(`{"id":"pj-9"}`)

			_, err := c.stdin.WriteString(printed)
			Expect(err).NotTo(HaveOccurred())

			Expect(c.run("api", "addPickJob", "--data", "-")).To(Equal(exitcode.OK))
			Expect(t.only().json()).To(HaveKey("pickLineItems"))
		})
	})

	Describe("fft api list", func() {
		It("enumerates the operations of a tag", func() {
			Expect(c.run("api", "list", "--tag", "Picking (Operations)")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("getPickJob"))
			Expect(c.out()).To(ContainSubstring("/api/pickjobs/{pickJobId}"))
		})

		It("matches a tag by substring, so the parentheses need not be retyped", func() {
			Expect(c.run("api", "list", "--tag", "picking")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("getPickJob"))
		})

		It("keeps the count on stderr, so -o json | jq sees only the array", func() {
			Expect(c.run("api", "list", "--tag", "picking", "-o", "json")).To(Equal(exitcode.OK))

			var ops []map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &ops)).To(Succeed())
			Expect(ops).NotTo(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("operations"))
		})

		It("filters by --search across id, path and summary", func() {
			Expect(c.run("api", "list", "--search", "handover")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("andover"))
			Expect(c.out()).NotTo(ContainSubstring("getPickJob\n"))
		})

		It("says so, on stderr, when nothing matches", func() {
			Expect(c.run("api", "list", "--search", "zzzznothing")).To(Equal(exitcode.OK))

			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("No operations found"))
		})

		It("prints [] for an empty result under -o json, so jq length answers 0", func() {
			Expect(c.run("api", "list", "--search", "zzzznothing", "-o", "json")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("[]"))
		})

		It("needs no project", func() {
			Expect(c.run("api", "list", "--search", "pickjob")).To(Equal(exitcode.OK))
		})
	})

	Describe("fft api describe", func() {
		It("shows the endpoint, the parameters and how to call it", func() {
			Expect(c.run("api", "describe", "getPickJob")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("GET /api/pickjobs/{pickJobId}"))
			Expect(c.out()).To(ContainSubstring("--pick-job-id"))
			Expect(c.out()).To(ContainSubstring("required"))
			Expect(c.out()).To(ContainSubstring("fft api getPickJob --param pickJobId="))
		})

		It("shows the permissions the endpoint needs", func() {
			Expect(c.run("api", "describe", "getStockSummaries")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("PERMISSIONS"))
			Expect(c.out()).To(ContainSubstring("STOCK_AVAILABILITIES_READ"))
		})

		It("shows a sample body for an operation that takes one", func() {
			Expect(c.run("api", "describe", "addPickJob")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("EXAMPLE BODY"))
			Expect(c.out()).To(ContainSubstring("tenantOrderId"))
		})

		It("says which encoding an array parameter uses, because it cannot be guessed", func() {
			Expect(c.run("api", "describe", "getStowJobs")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("comma-joined"))

			Expect(c.run("api", "describe", "getServiceJobs")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("repeated"))
		})

		It("gives -o json the whole record, explode flag and all", func() {
			Expect(c.run("api", "describe", "getStowJobs", "-o", "json")).To(Equal(exitcode.OK))

			var op struct {
				ID     string `json:"id"`
				Params []struct {
					Name    string `json:"name"`
					Explode bool   `json:"explode"`
				} `json:"params"`
			}
			Expect(json.Unmarshal([]byte(c.out()), &op)).To(Succeed())
			Expect(op.ID).To(Equal("getStowJobs"))

			var status struct {
				found   bool
				explode bool
			}
			for _, p := range op.Params {
				if p.Name == "status" {
					status.found, status.explode = true, p.Explode
				}
			}
			Expect(status.found).To(BeTrue())
			Expect(status.explode).To(BeFalse(), "the stowjob status is comma-joined")
		})

		It("exits 2 on an operationId that does not exist", func() {
			Expect(c.run("api", "describe", "nope")).To(Equal(exitcode.Usage))
		})
	})
})
