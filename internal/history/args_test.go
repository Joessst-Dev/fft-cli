package history_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/history"
)

var _ = Describe("the arguments an entry records", func() {
	DescribeTable("are written the way a shell would take them, and read back as given",
		func(positional, flags, recorded []string) {
			Expect(history.Args(positional, flags)).To(Equal(recorded))

			gotPositional, gotFlags := history.SplitArgs(recorded)
			Expect(gotPositional).To(Equal(positional))
			Expect(gotFlags).To(Equal(flags))
		},
		Entry("plain arguments before the flags",
			[]string{"BER-01"}, []string{"--all"},
			[]string{"BER-01", "--all"}),
		Entry("no arguments",
			[]string{}, []string{"--status=OPEN"},
			[]string{"--status=OPEN"}),
		Entry("an argument that starts with --, after the flags and a separator",
			[]string{"--BER-01"}, []string{"--all"},
			[]string{"--all", "--", "--BER-01"}),
		Entry("an argument that starts with a single dash",
			[]string{"BER-01", "-x"}, []string{},
			[]string{"--", "BER-01", "-x"}),
		Entry("an argument that is itself --",
			[]string{"--"}, []string{"--all"},
			[]string{"--all", "--", "--"}),
		Entry("stdin's dash, which a shell takes as an argument",
			[]string{"-"}, []string{"--all"},
			[]string{"-", "--all"}),
	)

	It("redacts what it records, a dashed argument as strictly as a flag", func() {
		Expect(history.Args([]string{"--token=abc"}, []string{`--data={"a":1}`})).
			To(Equal([]string{"--data=<redacted>", "--", "--token=<redacted>"}))
	})

	It("reads an entry written before the separator was", func() {
		positional, flags := history.SplitArgs([]string{"getPickJob", "--param=pickJobId=<redacted>", "--all"})
		Expect(positional).To(Equal([]string{"getPickJob"}))
		Expect(flags).To(Equal([]string{"--param=pickJobId=<redacted>", "--all"}))
	})

	It("leaves the recorded slice alone", func() {
		recorded := []string{"--all", "--", "--BER-01"}
		positional, _ := history.SplitArgs(recorded)
		positional[0] = "changed"
		Expect(recorded).To(Equal([]string{"--all", "--", "--BER-01"}))
	})
})
