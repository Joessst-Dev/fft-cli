package main

import (
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

func category(id, name, color string, version int) string {
	return fmt.Sprintf(`{"id":%q,"nameLocalized":{"en_US":%q},"color":%q,"version":%d}`, id, name, color, version)
}

func categoryPage(items []string, total int) string {
	return fmt.Sprintf(`{"routingStrategyNodeConfigCategories":[%s],"total":%d}`, strings.Join(items, ","), total)
}

var _ = Describe("fft routing category list", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("renders the categories", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, categoryPage([]string{
				category("k1", "Peak", "red", 3),
				category("k2", "Slow", "blue", 1),
			}, 2))
		})

		Expect(c.run("routing", "category", "list")).To(Equal(exitcode.OK))
		Expect(c.out()).To(Equal(strings.Join([]string{
			"ID   NAME   COLOR   VERSION",
			"k1   Peak   red     3",
			"k2   Slow   blue    1",
			"",
		}, "\n")))
	})

	It("reads the categories out of the routingStrategyNodeConfigCategories envelope", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, categoryPage(nil, 0))
		})

		Expect(c.run("routing", "category", "list")).To(Equal(exitcode.OK))
		Expect(api.only().Path).To(Equal("/api/routing/nodeconfigcategories"))
	})
})

var _ = Describe("fft routing category create", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("prints a spec-synthesized example with no network", func() {
		Expect(c.run("routing", "category", "create", "--example")).To(Equal(exitcode.OK))
		Expect(c.out()).To(ContainSubstring("color"))
		Expect(c.out()).To(ContainSubstring("nameLocalized"))
	})

	It("sends the body and reports the id", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, category("k-new", "Peak", "red", 1))
		})

		file := tempFile(`{"nameLocalized":{"en_US":"Peak"},"color":"red"}`)
		Expect(c.run("routing", "category", "create", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/routing/nodeconfigcategories"))
		Expect(c.errOut()).To(ContainSubstring("Created routing category k-new"))
	})
})

var _ = Describe("fft routing category update", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("reads the version and sends it back", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, category("k1", "Peak", "red", 6))
		})

		file := tempFile(category("k1", "Peak", "red", 6))
		Expect(c.run("routing", "category", "update", "k1", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))
		put := api.calls[1]
		Expect(put.Method).To(Equal(http.MethodPut))
		Expect(put.Path).To(Equal("/api/routing/nodeconfigcategories/k1"))
		Expect(put.json()).To(HaveKeyWithValue("version", BeNumerically("==", 6)))
	})
})

var _ = Describe("fft routing category delete", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	// A UUID is not a question anybody can answer; the category is read first so the
	// prompt can name it.
	It("names the category by name and colour, not by id", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, category("k1", "Peak", "red", 1))
		})
		c.answer("n")

		Expect(c.run("routing", "category", "delete", "k1")).To(Equal(exitcode.OK))
		Expect(c.errOut()).To(ContainSubstring(`the category "Peak" (red)`))
		Expect(c.errOut()).To(ContainSubstring("was not deleted"))
	})

	It("deletes without the read under --yes", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.WriteHeader(http.StatusOK)
		})

		Expect(c.run("routing", "category", "delete", "k1", "--yes")).To(Equal(exitcode.OK))
		Expect(api.only().Method).To(Equal(http.MethodDelete))
		Expect(api.only().Path).To(Equal("/api/routing/nodeconfigcategories/k1"))
	})

	It("refuses rather than assuming yes when there is nobody to ask", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, category("k1", "Peak", "red", 1))
		})

		Expect(c.run("routing", "category", "delete", "k1")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("--yes"))
	})
})

var _ = Describe("fft routing category read-only refusals", func() {
	var c *cli
	var api *tenant

	BeforeEach(func() {
		c = newCLI()
		c.setenv(config.EnvReadOnly, "1")
		api = c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " under --read-only")
		})
	})

	AfterEach(func() { Expect(api.calls).To(BeEmpty()) })

	It("refuses delete", func() {
		Expect(c.run("routing", "category", "delete", "k1", "--yes")).To(Equal(exitcode.ReadOnly))
	})
})
