package exitcode_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("Meaning", func() {
	DescribeTable("describes every code fft exits with",
		func(code int, meaning string) {
			Expect(exitcode.Meaning(code)).To(Equal(meaning))
		},
		Entry("ok", exitcode.OK, "success"),
		Entry("auth", exitcode.Auth, "authentication failed"),
		Entry("read-only", exitcode.ReadOnly, "read-only: fft refused a write, nothing was sent"),
		Entry("interrupted", exitcode.Interrupted, "interrupted"),
	)

	It("has a description for each of the documented codes", func() {
		for _, code := range []int{
			exitcode.OK, exitcode.General, exitcode.Usage, exitcode.Config, exitcode.Auth,
			exitcode.Forbidden, exitcode.NotFound, exitcode.Conflict, exitcode.Partial,
			exitcode.Unavailable, exitcode.ReadOnly, exitcode.Interrupted,
		} {
			Expect(exitcode.Meaning(code)).NotTo(Equal("unknown exit code"), "exit code %d", code)
		}
	})

	It("says so for a code fft never exits with", func() {
		Expect(exitcode.Meaning(42)).To(Equal("unknown exit code"))
	})
})
