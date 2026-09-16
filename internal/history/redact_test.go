package history_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/history"
)

var _ = Describe("redacting a command line", func() {
	DescribeTable("keeps what says what was done, and drops what could be a secret or a body",
		func(arg, want string) {
			Expect(history.Redact([]string{arg})).To(Equal([]string{want}))
		},
		Entry("a positional argument", "getPickJob", "getPickJob"),
		Entry("a plain flag", "--status=OPEN", "--status=OPEN"),
		Entry("a flag without a value", "--all", "--all"),
		Entry("an inline body", `--data={"name":"x"}`, "--data=<redacted>"),
		Entry("an inline body that is not an object", "--data=hello", "--data=<redacted>"),
		Entry("a body from stdin", "--data=-", "--data=-"),
		Entry("a body from a file", "--data=@body.json", "--data=@body.json"),
		Entry("a header", "--header=Authorization: Bearer abc", "--header=Authorization: <redacted>"),
		Entry("a header written as a pair", "--header=X-Trace=abc", "--header=X-Trace=<redacted>"),
		Entry("a template value", "--set=facility=BER-01", "--set=facility=<redacted>"),
		Entry("a path parameter", "--param=pickJobId=pj-1", "--param=pickJobId=<redacted>"),
		Entry("a required template parameter", "--require=name=path.to", "--require=name=<redacted>"),
		Entry("a pair flag with no pair in it", "--header=garbage", "--header=<redacted>"),
		Entry("a credential-named flag", "--firebase-api-key=AIzaSy", "--firebase-api-key=<redacted>"),
		Entry("a credential-named query parameter", "--query=access_token=abc", "--query=access_token=<redacted>"),
		Entry("an ordinary query parameter", "--query=status=OPEN", "--query=status=OPEN"),
		Entry("a JSON value in any flag", `--filter=[{"a":1}]`, "--filter=<redacted>"),
		Entry("a JSON positional argument", `{"a":1}`, "<redacted>"),
	)

	It("leaves the caller's slice alone", func() {
		args := []string{`--data={"a":1}`}
		history.Redact(args)
		Expect(args).To(Equal([]string{`--data={"a":1}`}))
	})
})
