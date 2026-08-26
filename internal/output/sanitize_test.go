package output_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

var _ = Describe("Sanitize", func() {
	DescribeTable("stripping what a terminal would act on rather than draw",
		func(in, want string) {
			Expect(output.Sanitize(in)).To(Equal(want))
		},
		Entry("plain text is untouched", "harmless", "harmless"),
		Entry("a carriage return that would overwrite the line above", "a\rb", "ab"),
		Entry("an ESC that would start a CSI sequence", "a\x1b[31mb", "a[31mb"),
		Entry("a BEL", "a\ab", "ab"),
		Entry("a C1 control code point", "a"+string(rune(0x9b))+"b", "ab"),
		Entry("tab and newline survive, since multi-line text stays multi-line",
			"a\tb\nc", "a\tb\nc"),
	)
})

var _ = Describe("SanitizeCell", func() {
	DescribeTable("keeping a value inside the one line its cell occupies",
		func(in, want string) {
			Expect(output.SanitizeCell(in)).To(Equal(want))
		},
		Entry("plain text is untouched", "harmless", "harmless"),
		Entry("a newline that would forge a second row", "real\nforged", "real forged"),
		Entry("a tab, which is the column separator itself", "a\tb", "a b"),
		Entry("the control bytes Sanitize strips are still stripped",
			"a\rb\x1b[31mc", "ab[31mc"),
		Entry("a run of newlines stays one cell", "a\n\nb", "a  b"),
	)

	It("strips rather than folds a carriage return, exactly as Sanitize does", func() {
		Expect(output.SanitizeCell("a\rb")).To(Equal("ab"))
	})
})
