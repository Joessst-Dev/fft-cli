package component

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("scaffold command secret masking", func() {
	// The structural guard: every command scaffold, in every language in the
	// canonical supportedLangs list, names each scaffoldSecretEnv variable as a whole
	// word (so FFT_ID_TOKEN is not spuriously satisfied by FFT_ID_TOKEN_EXPIRES_AT),
	// and a new language that forgets one is caught because this drives the real
	// Build path.
	It("names every scaffoldSecretEnv variable, as a whole word, in every language", func() {
		for _, lang := range supportedLangs {
			files, err := Scaffold{Name: "widget", Kind: KindCommand, Lang: lang}.Build()
			Expect(err).NotTo(HaveOccurred(), "lang %s", lang)

			var body string
			for _, f := range files {
				if f.Name == "bin/fft-widget" || f.Name == "main.go" {
					body = string(f.Data)
				}
			}
			Expect(body).NotTo(BeEmpty(), "lang %s produced no executable source", lang)

			for _, name := range scaffoldSecretEnv {
				whole := regexp.MustCompile(regexp.QuoteMeta(name) + `\b`)
				Expect(whole.MatchString(body)).To(BeTrue(), "lang %s does not name %s as a whole word", lang, name)
			}
		}
	})

	// The behavioural guard, for the always-runnable shell scaffold: presence of the
	// name is not proof of masking, so run it with real secret values and assert the
	// values are actually redacted while a non-secret FFT_ variable passes through.
	//
	// Only shell is exercised behaviourally — it is the one language guaranteed on
	// every runner. The go/python/node templates rest on the structural guard above;
	// running each would need its toolchain present and is left to a manual check.
	It("actually redacts the credential values when the shell scaffold runs", func() {
		if runtime.GOOS == "windows" {
			Skip("no POSIX shell to run the scaffold with")
		}

		files, err := Scaffold{Name: "widget", Kind: KindCommand, Lang: LangShell}.Build()
		Expect(err).NotTo(HaveOccurred())

		script := filepath.Join(GinkgoT().TempDir(), "fft-widget")
		for _, f := range files {
			if f.Name == "bin/fft-widget" {
				Expect(os.WriteFile(script, f.Data, 0o755)).To(Succeed())
			}
		}

		cmd := exec.Command("/bin/sh", script)
		cmd.Env = append(os.Environ(),
			"FFT_ID_TOKEN=SECRET-ID",
			"FFT_REFRESH_TOKEN=SECRET-REFRESH",
			"FFT_PASSWORD=SECRET-PW",
			"FFT_FIREBASE_API_KEY=SECRET-KEY",
			"FFT_PROJECT_ID=acme", // not a secret — must pass through unredacted
		)
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(out))

		Expect(string(out)).To(ContainSubstring("<redacted>"))
		Expect(string(out)).To(ContainSubstring("FFT_PROJECT_ID=acme"))
		for _, secret := range []string{"SECRET-ID", "SECRET-REFRESH", "SECRET-PW", "SECRET-KEY"} {
			Expect(string(out)).NotTo(ContainSubstring(secret), "the shell scaffold leaked a credential value")
		}
	})
})
