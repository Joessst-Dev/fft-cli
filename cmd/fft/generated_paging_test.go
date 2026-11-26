package main

import (
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("generated list commands", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	ids := func(names ...string) string {
		items := make([]string, len(names))
		for i, n := range names {
			items[i] = fmt.Sprintf(`{"id":%q}`, n)
		}
		return strings.Join(items, ",")
	}

	// GET /api/categories declares `size` with maximum 100 and answers a bare array.
	Describe("the spec's size maximum", func() {
		It("refuses --size above it before sending anything", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, "[]")
			})

			Expect(c.run("category", "get-categories", "--size", "250")).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring("size must be between 1 and 100, not 250"))
			Expect(api.calls).To(BeEmpty())
		})

		It("is enforced for the escape hatch too", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, "[]")
			})

			Expect(c.run("api", "getCategories", "--query", "size=250")).To(Equal(exitcode.Usage))
			Expect(api.calls).To(BeEmpty())
		})

		It("still passes a size inside the range through", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, "[]")
			})

			Expect(c.run("category", "get-categories", "--size", "100")).To(Equal(exitcode.OK))
			Expect(api.calls[0].Query.Get("size")).To(Equal("100"))
		})

		It("is shown in --help", func() {
			Expect(c.run("category", "get-categories", "--help")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("(between 1 and 100)"))
		})
	})

	Describe("--all on a bare-array list", func() {
		It("starts each page after the last id of the one before and stops on a short page", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
				switch r.URL.Query().Get("startAfterId") {
				case "":
					writeJSON(w, http.StatusOK, "["+ids("a", "b")+"]")
				default:
					writeJSON(w, http.StatusOK, "["+ids("c")+"]")
				}
			})

			Expect(c.run("-o", "json", "category", "get-categories", "--all", "--size", "2")).To(Equal(exitcode.OK))

			Expect(api.calls).To(HaveLen(2))
			Expect(api.calls[0].Query).NotTo(HaveKey("startAfterId"))
			Expect(api.calls[1].Query.Get("startAfterId")).To(Equal("b"))
			Expect(c.out()).To(MatchJSON(`[{"id":"a"},{"id":"b"},{"id":"c"}]`))
		})

		It("pages by the spec's maximum, not by a size the API would refuse", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, "["+ids("a")+"]")
			})

			Expect(c.run("category", "get-categories", "--all")).To(Equal(exitcode.OK))
			Expect(api.calls[0].Query.Get("size")).To(Equal("100"))
		})

		It("truncates at --max-items, warns on stderr and still exits 0", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, "["+ids("a", "b")+"]")
			})

			Expect(c.run("-o", "json", "category", "get-categories",
				"--all", "--size", "2", "--max-items", "2")).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("stopped after 2 items"))
			Expect(c.out()).To(MatchJSON(`[{"id":"a"},{"id":"b"}]`))
		})

		It("prints [] for an empty list under -o json", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, "[]")
			})

			Expect(c.run("-o", "json", "category", "get-categories", "--all")).To(Equal(exitcode.OK))
			Expect(c.out()).To(MatchJSON(`[]`))
		})

		It("refuses --start-after-id, because --all pages from the start", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, "[]")
			})

			Expect(c.run("category", "get-categories", "--all", "--start-after-id", "x")).To(Equal(exitcode.Usage))
			Expect(api.calls).To(BeEmpty())
		})

		It("refuses --max-items without --all, which would do nothing", func() {
			Expect(c.run("category", "get-categories", "--max-items", "5")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("--max-items only means something with --all"))
		})
	})

	// GET /api/packagingunits/details answers {"packagingUnits": [...], "total": n}
	// and caps `size` at 25.
	Describe("--all on an envelope list", func() {
		page := func(total int, names ...string) string {
			return fmt.Sprintf(`{"packagingUnits":[%s],"total":%d}`, ids(names...), total)
		}

		It("reads the entities from the envelope key the spec names", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, page(1, "p1"))
			})

			Expect(c.run("-o", "json", "packaging-unit", "get-all-packaging-unit-details", "--all")).To(Equal(exitcode.OK))

			Expect(api.calls[0].Query.Get("size")).To(Equal("25"))
			Expect(c.out()).To(MatchJSON(`[{"id":"p1"}]`))
		})

		// A server that caps `size` below what was asked hands back a short first page;
		// the envelope's total is what says the list did not end there.
		It("keeps going past a short page while the total says there is more", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
				if r.URL.Query().Get("startAfterId") == "p1" {
					writeJSON(w, http.StatusOK, page(2, "p2"))
					return
				}
				writeJSON(w, http.StatusOK, page(2, "p1"))
			})

			Expect(c.run("-o", "json", "packaging-unit", "get-all-packaging-unit-details", "--all", "--size", "10")).To(Equal(exitcode.OK))

			Expect(api.calls).To(HaveLen(2))
			Expect(c.out()).To(MatchJSON(`[{"id":"p1"},{"id":"p2"}]`))
		})
	})

	// GET /api/operationsvalidations answers an envelope and caps `size` at 500,
	// which is above the 250 fft itself will fetch per request.
	Describe("--all with a size between fft's ceiling and the spec's maximum", func() {
		It("refuses it as a usage error naming --size and the effective range", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, `{"operationsValidations":[],"total":0}`)
			})

			Expect(c.run("operations-validation", "get-all-operations-validations", "--all", "--size", "300")).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring("--size with --all must be between 1 and 250, not 300"))
			Expect(api.calls).To(BeEmpty())
		})

		It("still accepts that size for a single page", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, `{"operationsValidations":[],"total":0}`)
			})

			Expect(c.run("operations-validation", "get-all-operations-validations", "--size", "300")).To(Equal(exitcode.OK))
			Expect(api.calls[0].Query.Get("size")).To(Equal("300"))
		})

		// getCategories declares `size` as `type: number`, so the flag takes 2.5 and
		// the bounds check passes it. fft pages by whole items: silently falling back
		// to the default page would do something other than what was asked.
		It("refuses a fractional size as a usage error naming --size", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, `{"categories":[],"total":0}`)
			})

			Expect(c.run("category", "get-categories", "--all", "--size", "2.5")).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring("--size with --all must be a whole number, not 2.5"))
			Expect(api.calls).To(BeEmpty())
		})

		It("accepts the largest size fft fetches", func() {
			api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, `{"operationsValidations":[],"total":0}`)
			})

			Expect(c.run("operations-validation", "get-all-operations-validations", "--all", "--size", "250")).To(Equal(exitcode.OK))
			Expect(api.calls[0].Query.Get("size")).To(Equal("250"))
		})
	})

	It("offers no --all on a list whose answer fft cannot read entities from", func() {
		// getAllUsers carries three arrays and no total.
		Expect(c.run("user-management", "get-all-users", "--help")).To(Equal(exitcode.OK))
		Expect(c.out()).NotTo(ContainSubstring("--all"))
	})
})
