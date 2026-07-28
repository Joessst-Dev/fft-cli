package component

import (
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This keeps every command scaffold in step with scaffoldSecretEnv. It drives the
// real Build path for each language in supportedLangs — so a newly-added language
// whose template forgets to mask a credential is caught, not just the four that
// exist today — and matches each name as a whole word, so "FFT_ID_TOKEN" is not
// spuriously satisfied by the unrelated (and non-secret) FFT_ID_TOKEN_EXPIRES_AT
// appearing in the environment dump.
var _ = Describe("scaffold command secret masking", func() {
	It("masks every scaffoldSecretEnv name, as a whole word, in every language", func() {
		for _, lang := range supportedLangs {
			files, err := Scaffold{Name: "widget", Kind: KindCommand, Lang: lang}.Build()
			Expect(err).NotTo(HaveOccurred(), "lang %s", lang)

			// The executable source is the file that dumps the environment: bin/<exec>
			// for a script, main.go for Go.
			var body string
			for _, f := range files {
				if f.Name == "bin/fft-widget" || f.Name == "main.go" {
					body = string(f.Data)
				}
			}
			Expect(body).NotTo(BeEmpty(), "lang %s produced no executable source", lang)

			for _, name := range scaffoldSecretEnv {
				// \b after the name refuses a match inside a longer identifier: `_` is
				// a word char, so `FFT_ID_TOKEN\b` does not match in `FFT_ID_TOKEN_EXPIRES`.
				whole := regexp.MustCompile(regexp.QuoteMeta(name) + `\b`)
				Expect(whole.MatchString(body)).To(BeTrue(), "lang %s does not mask %s as a whole name", lang, name)
			}
		}
	})
})
