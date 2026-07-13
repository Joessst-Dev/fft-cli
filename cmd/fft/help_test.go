package main

import (
	"net/http"
	"strings"
	"unicode/utf8"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("the spec-aware help", func() {
	Describe("truncateAt", func() {
		// The spec's prose is full of curly quotes and em-dashes, and a byte-index cut
		// through one of them emits a broken rune into --help.
		It("never splits a UTF-8 rune", func() {
			// "—" is three bytes, so a cut at 13, 14 or 15 lands inside it.
			s := "the platform—wide identifier"

			for n := range len(s) {
				out := truncateAt(s, n)

				Expect(utf8.ValidString(out)).To(BeTrue(),
					"truncateAt(%q, %d) produced invalid UTF-8: %q", s, n, out)
			}
		})

		It("leaves a string that already fits alone", func() {
			Expect(truncateAt("short", 40)).To(Equal("short"))
		})

		It("marks that it cut something", func() {
			Expect(truncateAt("a much longer sentence than this", 10)).To(HaveSuffix("…"))
		})

		It("survives every parameter description in the real spec", func() {
			for _, op := range api.Operations() {
				for _, p := range op.Params {
					Expect(utf8.ValidString(truncateAt(p.Description, 100))).To(BeTrue(),
						"%s/%s", op.ID, p.Name)
				}
			}
		})
	})

	Describe("a deprecated operation's notice", func() {
		It("goes to stderr, so it cannot contaminate -o json", func() {
			// Cobra prints a deprecation notice on every run, and 21 operations in the
			// spec are deprecated. If it ever landed on stdout, `fft ... -o json | jq`
			// would break for exactly those — and only for those, which is the kind of bug
			// that ships.
			c := newCLI()

			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{"articles":[]}`))
				Expect(err).NotTo(HaveOccurred())
			})

			deprecated, ok := api.LookupOperation("getArticles")
			Expect(ok).To(BeTrue())
			Expect(deprecated.Deprecated).To(BeTrue(), "getArticles is no longer deprecated; pick another")

			// getArticles is tagged Stocks (Inventory), so it lands under the curated
			// `fft stock` group alongside the hand-written commands.
			Expect(c.run("stock", "get-articles", "-o", "json")).To(Equal(exitcode.OK))

			Expect(c.out()).NotTo(ContainSubstring("deprecated"))
			Expect(c.errOut()).To(ContainSubstring("deprecated"))
		})
	})

	Describe("wrap", func() {
		It("breaks on word boundaries and never mid-word", func() {
			out := wrap("the quick brown fox jumps over the lazy dog", 12)

			Expect(out).To(ContainSubstring("\n"))
			for _, line := range strings.Split(out, "\n") {
				Expect(line).NotTo(BeEmpty())
				Expect(len(line)).To(BeNumerically("<=", 12))
			}
		})

		It("returns nothing for nothing", func() {
			Expect(wrap("", 40)).To(BeEmpty())
		})
	})
})
