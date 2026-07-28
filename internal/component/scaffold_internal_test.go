package component

import (
	"regexp"
	"testing"
)

// TestScaffoldCommandsMaskEverySecretEnv keeps every command scaffold in step with
// scaffoldSecretEnv. It drives the real Build path for each language in
// supportedLangs — so a newly-added language whose template forgets to mask a
// credential is caught, not just the four that exist today — and matches each name
// as a whole word, so "FFT_ID_TOKEN" is not spuriously satisfied by the unrelated
// (and non-secret) FFT_ID_TOKEN_EXPIRES_AT appearing in the environment dump.
func TestScaffoldCommandsMaskEverySecretEnv(t *testing.T) {
	for _, lang := range supportedLangs {
		files, err := Scaffold{Name: "widget", Kind: KindCommand, Lang: lang}.Build()
		if err != nil {
			t.Fatalf("build %s command scaffold: %v", lang, err)
		}

		// The executable source is the file that dumps the environment: bin/<exec>
		// for a script, main.go for Go.
		var body string
		for _, f := range files {
			if f.Name == "bin/fft-widget" || f.Name == "main.go" {
				body = string(f.Data)
			}
		}
		if body == "" {
			t.Fatalf("%s scaffold produced no executable source", lang)
		}

		for _, name := range scaffoldSecretEnv {
			// \b after the name refuses a match inside a longer identifier: `_` is a
			// word char, so `FFT_ID_TOKEN\b` does not match in `FFT_ID_TOKEN_EXPIRES`.
			whole := regexp.MustCompile(regexp.QuoteMeta(name) + `\b`)
			if !whole.MatchString(body) {
				t.Errorf("%s command scaffold does not mask %s as a whole name", lang, name)
			}
		}
	}
}
