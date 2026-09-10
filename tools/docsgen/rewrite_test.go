package main

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("rewriteLinks", func() {
	It("resolves an intra-skill file link to its guide page", func() {
		Expect(rewriteLinks("see [r](references/recipes.md)")).
			To(Equal("see [r](./recipes.md)"))
	})

	It("carries a fragment across to the guide page", func() {
		Expect(rewriteLinks("see [r](references/emulator.md#eventing)")).
			To(Equal("see [r](./emulator.md#eventing)"))
	})

	// A skill file links to its own headings. Resolving those against anything
	// but the page itself is how they used to end up pointing at README anchors
	// that never existed.
	It("leaves a fragment-only link alone", func() {
		Expect(rewriteLinks("see [x](#ids-are-not-numbers)")).
			To(Equal("see [x](#ids-are-not-numbers)"))
	})

	It("leaves an external link untouched", func() {
		Expect(rewriteLinks("see [c](https://sigstore.dev)")).
			To(Equal("see [c](https://sigstore.dev)"))
	})

	It("leaves a link to a file that is not a skill page untouched", func() {
		Expect(rewriteLinks("see [l](LICENSE)")).To(Equal("see [l](LICENSE)"))
	})

	It("does not rewrite a literal '](...)' inside a fenced code block", func() {
		body := "see [r](references/recipes.md)\n```\n[not a link](references/recipes.md)\n```\nsee [r](references/recipes.md)"
		Expect(rewriteLinks(body)).To(Equal(
			"see [r](./recipes.md)\n```\n[not a link](references/recipes.md)\n```\nsee [r](./recipes.md)",
		))
	})
})

var _ = Describe("sweep", func() {
	var dir string

	// A page carrying a source key is docsgen's; one without it is hand-written.
	write := func(name, frontMatter string) string {
		p := filepath.Join(dir, name)
		Expect(os.WriteFile(p, []byte(frontMatter), 0o600)).To(Succeed())
		return p
	}

	BeforeEach(func() { dir = GinkgoT().TempDir() })

	It("deletes a generated page this run did not write", func() {
		orphan := write("retired.md", "---\ntitle: Retired\nsource: internal/skill/assets/references/retired.md\n---\n\nbody\n")
		Expect(sweep(dir, map[string]bool{})).To(Succeed())
		Expect(orphan).NotTo(BeAnExistingFile())
	})

	It("keeps a generated page this run wrote", func() {
		kept := write("recipes.md", "---\ntitle: Recipes\nsource: internal/skill/assets/references/recipes.md\n---\n\nbody\n")
		Expect(sweep(dir, map[string]bool{"recipes.md": true})).To(Succeed())
		Expect(kept).To(BeARegularFile())
	})

	It("keeps a hand-written page, which has no source key", func() {
		kept := write("auth.md", "---\ntitle: Authentication\n---\n\nbody\n")
		Expect(sweep(dir, map[string]bool{})).To(Succeed())
		Expect(kept).To(BeARegularFile())
	})

	It("does not read a 'source:' line in the body as ownership", func() {
		kept := write("install.md", "---\ntitle: Install\n---\n\nsource: not front matter\n")
		Expect(sweep(dir, map[string]bool{})).To(Succeed())
		Expect(kept).To(BeARegularFile())
	})
})
