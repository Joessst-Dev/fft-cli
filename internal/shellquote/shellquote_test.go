package shellquote_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/shellquote"
)

// The shells' own reading of these spellings is proven in internal/tui, which
// runs them through sh; these pin the spelling a printed command carries.
var _ = Describe("quoting a value for a pasted command line", func() {
	DescribeTable("spells it so that every shell reads back the value",
		func(value, want string, portable bool) {
			got, ok := shellquote.Quote(value)

			Expect(got).To(Equal(want))
			Expect(ok).To(Equal(portable))
		},
		Entry("leaves a plain id bare", "BER-01", "BER-01", true),
		Entry("quotes a command substitution", "BER-01;$(id)", "'BER-01;$(id)'", true),
		Entry("quotes a space", "orderType=same day", "'orderType=same day'", true),
		Entry("quotes the empty value", "", "''", true),
		Entry("has no portable spelling for a single quote", "BER-'01", `'BER-'\''01'`, false),
	)
})
