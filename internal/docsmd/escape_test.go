package docsmd_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/docsmd"
)

var _ = Describe("EscapeAngles", func() {
	It("escapes a bare '<' in prose so VitePress does not read it as a tag", func() {
		Expect(docsmd.EscapeAngles("there is no fft stock actions <id>")).
			To(Equal("there is no fft stock actions &lt;id>"))
	})

	It("leaves '>' alone — it opens no tag, and touching it would break a blockquote", func() {
		Expect(docsmd.EscapeAngles("> a quote with <x> in it")).
			To(Equal("> a quote with &lt;x> in it"))
	})

	It("does not touch an inline code span, where Markdown already escapes the brackets", func() {
		Expect(docsmd.EscapeAngles("the `<id>` placeholder")).
			To(Equal("the `<id>` placeholder"))
	})

	It("escapes outside a code span while sparing the span on the same line", func() {
		Expect(docsmd.EscapeAngles("pass <id>, not the `<urn>` form")).
			To(Equal("pass &lt;id>, not the `<urn>` form"))
	})

	It("leaves a fenced code block untouched", func() {
		in := "before <a>\n```\ncode <b> line\n```\nafter <c>"
		Expect(docsmd.EscapeAngles(in)).
			To(Equal("before &lt;a>\n```\ncode <b> line\n```\nafter &lt;c>"))
	})

	It("treats an indented closing fence as a fence boundary", func() {
		in := "```sh\n<kept>\n```\n<escaped>"
		Expect(docsmd.EscapeAngles(in)).
			To(Equal("```sh\n<kept>\n```\n&lt;escaped>"))
	})

	It("is a no-op on text with no angle brackets", func() {
		Expect(docsmd.EscapeAngles("plain text, nothing to do")).
			To(Equal("plain text, nothing to do"))
	})
})
