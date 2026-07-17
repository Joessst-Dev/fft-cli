package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("slug", func() {
	It("keeps runs of hyphens, the way GitHub anchors a heading", func() {
		Expect(slug("On Windows, `--no-keyring` protects")).
			To(Equal("on-windows---no-keyring-protects"))
	})

	It("collapses runs for the VitePress anchor", func() {
		Expect(vitepressSlug("On Windows, `--no-keyring` protects")).
			To(Equal("on-windows-no-keyring-protects"))
	})
})

var _ = Describe("linkRewriter", func() {
	// A README with two of the real page headings and a sub-heading whose slug has
	// a hyphen run, so the GitHub key and the VitePress fragment genuinely differ.
	const readme = "# fft\n\n" +
		"## Setting up a project\n\nbody\n\n" +
		"## Authentication, honestly\n\n" +
		"### On `--foo` bar\n\ntext\n"

	var rewrite func(string) string

	BeforeEach(func() {
		anchors, _, err := indexReadme(readme)
		Expect(err).NotTo(HaveOccurred())
		rewrite = linkRewriter(anchors, "https://example.com/repo")
	})

	It("points an anchor to a page's top heading at the page, no fragment", func() {
		Expect(rewrite("see [x](#authentication-honestly)")).
			To(Equal("see [x](./auth.md)"))
	})

	It("rewrites a sub-heading anchor to the page with the VitePress fragment", func() {
		Expect(rewrite("see [x](#on---foo-bar)")).
			To(Equal("see [x](./auth.md#on-foo-bar)"))
	})

	It("falls back to the README on GitHub for a section that is not a page", func() {
		Expect(rewrite("see [x](#exit-codes)")).
			To(Equal("see [x](https://example.com/repo/blob/main/README.md#exit-codes)"))
	})

	It("resolves an intra-skill file link to its guide page", func() {
		Expect(rewrite("see [r](references/recipes.md)")).
			To(Equal("see [r](./recipes.md)"))
	})

	It("leaves an external link untouched", func() {
		Expect(rewrite("see [c](https://sigstore.dev)")).
			To(Equal("see [c](https://sigstore.dev)"))
	})

	It("does not rewrite a literal '](...)' inside a fenced code block", func() {
		body := "see [x](#authentication-honestly)\n```\n[not a link](#exit-codes)\n```\nsee [x](#authentication-honestly)"
		Expect(rewrite(body)).To(Equal(
			"see [x](./auth.md)\n```\n[not a link](#exit-codes)\n```\nsee [x](./auth.md)",
		))
	})
})

var _ = Describe("indexReadme", func() {
	It("does not read a '## '-shaped line inside a ~~~ fence as a real heading", func() {
		readme := "# fft\n\n" +
			"## Setting up a project\n\n" +
			"~~~\n## not a real heading\n~~~\n" +
			"body\n"
		_, sections, err := indexReadme(readme)
		Expect(err).NotTo(HaveOccurred())
		Expect(sections["Setting up a project"]).To(ContainSubstring("## not a real heading"))
	})
})

var _ = Describe("shiftHeadings", func() {
	It("leaves a '##'-shaped line inside a ~~~ fence untouched", func() {
		in := "## Real Heading\n\n~~~\n## looks like a heading\n~~~\n"
		Expect(shiftHeadings(in)).To(Equal(
			"# Real Heading\n\n~~~\n## looks like a heading\n~~~\n",
		))
	})
})
