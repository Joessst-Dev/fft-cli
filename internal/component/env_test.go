package component_test

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/config"
)

var _ = Describe("the environment a component is given", func() {
	// The caller's environment: a developer with a full headless session exported in
	// their shell, plus the ordinary machine variables.
	caller := []string{
		"PATH=/usr/bin",
		"HOME=/home/dev",
		"PUBSUB_EMULATOR_HOST=localhost:8085",
		config.EnvBaseURL + "=https://real.tenant",
		config.EnvPassword + "=the-real-password",
		config.EnvFirebaseAPIKey + "=AIzaSyREAL",
		config.EnvIDToken + "=a-real-token",
		config.EnvReadOnly + "=",
	}

	session := &component.SessionInfo{
		BaseURL:   "https://tenant.example",
		Email:     "dev@example.com",
		Token:     "minted-token",
		Tenant:    "acme-tenant",
		ProjectID: "ocff-acme-dev",
	}

	// env renders what Environ produced into a map, so a spec can ask about one name.
	env := func(c component.Component, cmd component.Command, opts component.EnvOptions) map[string]string {
		GinkgoHelper()

		out, err := component.Environ(caller, c, cmd, opts)
		Expect(err).NotTo(HaveOccurred())

		got := map[string]string{}
		for _, entry := range out {
			name, value, _ := strings.Cut(entry, "=")
			got[name] = value
		}
		return got
	}

	comp := component.Component{Manifest: component.Manifest{Name: "weather"}}

	Describe("session: none", func() {
		cmd := component.Command{Name: "weather", Session: component.SessionNone}

		It("passes on nothing from the FFT_ namespace but what it builds itself", func() {
			got := env(comp, cmd, component.EnvOptions{Root: "/root", Version: "1.2.3"})

			// The whole boundary in four assertions: the caller's own session does not
			// leak, in any of the forms it can take.
			Expect(got).NotTo(HaveKey(config.EnvPassword))
			Expect(got).NotTo(HaveKey(config.EnvIDToken))
			Expect(got).NotTo(HaveKey(config.EnvFirebaseAPIKey))
			Expect(got).NotTo(HaveKey(config.EnvBaseURL))
		})

		It("strips the FFT_ namespace whatever its case", func() {
			// Windows environment lookups are case-insensitive, so a lowercase
			// fft_password left behind by a case-sensitive strip is one a child reads as
			// FFT_PASSWORD. The strip must not depend on the case a variable was written in.
			got, err := component.Environ(
				[]string{"fft_password=leaked", "Fft_Id_Token=leaked", "PATH=/usr/bin"},
				comp, cmd, component.EnvOptions{},
			)
			Expect(err).NotTo(HaveOccurred())

			// No entry carries the leaked value, whatever case its name was written in —
			// while PATH and fft's own FFT_COMPONENT_* additions are untouched.
			for _, entry := range got {
				Expect(entry).NotTo(ContainSubstring("=leaked"), "a mixed-case FFT_ variable survived: %s", entry)
			}
			Expect(got).To(ContainElement("PATH=/usr/bin"))
		})

		It("keeps everything outside the FFT_ namespace", func() {
			got := env(comp, cmd, component.EnvOptions{})

			// PATH and HOME belong to the machine, not to fft. So does the variable a
			// Pub/Sub client reads: stripping those would leave a component unable to find
			// anything at all.
			Expect(got).To(HaveKeyWithValue("PATH", "/usr/bin"))
			Expect(got).To(HaveKeyWithValue("HOME", "/home/dev"))
			Expect(got).To(HaveKeyWithValue("PUBSUB_EMULATOR_HOST", "localhost:8085"))
		})

		It("says which contract and which component this is", func() {
			got := env(comp, cmd, component.EnvOptions{Root: "/root", Version: "1.2.3"})

			Expect(got).To(HaveKeyWithValue(component.EnvAPI, "1"))
			Expect(got).To(HaveKeyWithValue(component.EnvName, "weather"))
			Expect(got).To(HaveKeyWithValue(component.EnvVersion, "1.2.3"))
			Expect(got).To(HaveKeyWithValue(component.EnvRoot, "/root"))
		})

		It("forwards the caller's output preferences", func() {
			got := env(comp, cmd, component.EnvOptions{
				Output: "json", NoColor: true, Timeout: 45 * time.Second,
			})

			Expect(got).To(HaveKeyWithValue("FFT_OUTPUT", "json"))
			Expect(got).To(HaveKeyWithValue("FFT_NO_COLOR", "1"))
			Expect(got).To(HaveKeyWithValue("FFT_TIMEOUT", "45s"))
		})
	})

	Describe("session: read", func() {
		cmd := component.Command{Name: "weather", Session: component.SessionRead}

		It("hands over a token, and a placeholder where the API key would be", func() {
			got := env(comp, cmd, component.EnvOptions{Session: session})

			Expect(got).To(HaveKeyWithValue(config.EnvBaseURL, "https://tenant.example"))
			Expect(got).To(HaveKeyWithValue(config.EnvIDToken, "minted-token"))
			Expect(got).To(HaveKeyWithValue(config.EnvEmail, "dev@example.com"))

			// The key that mints fresh tokens is never handed over — not the caller's, and
			// not any other. What is there is enough for config.FromEnv to synthesize a
			// project and nothing more.
			Expect(got[config.EnvFirebaseAPIKey]).NotTo(Equal("AIzaSyREAL"))
			Expect(got[config.EnvFirebaseAPIKey]).NotTo(BeEmpty())
		})

		It("forces read-only", func() {
			got := env(comp, cmd, component.EnvOptions{Session: session})
			Expect(got).To(HaveKeyWithValue(config.EnvReadOnly, "1"))
		})

		It("refuses to run without a resolved session", func() {
			_, err := component.Environ(caller, comp, cmd, component.EnvOptions{})
			Expect(err).To(MatchError(ContainSubstring("needs a read session")))
		})
	})

	Describe("session: write", func() {
		cmd := component.Command{Name: "weather", Session: component.SessionWrite, Mutates: true}

		It("does not force read-only", func() {
			got := env(comp, cmd, component.EnvOptions{Session: session})

			Expect(got).To(HaveKeyWithValue(config.EnvIDToken, "minted-token"))
			Expect(got).NotTo(HaveKey(config.EnvReadOnly))
		})

		It("propagates the caller's own read-only state", func() {
			readOnly := *session
			readOnly.ReadOnly = true

			// The gate has already refused a mutating command by the time this is built.
			// This is the belt to its braces: a component holding a write session under a
			// read-only project still refuses its own writes.
			got := env(comp, cmd, component.EnvOptions{Session: &readOnly})
			Expect(got).To(HaveKeyWithValue(config.EnvReadOnly, "1"))
		})
	})

	Describe("the variables a manifest declares", func() {
		cmd := component.Command{Name: "weather", Session: component.SessionNone}
		declared := component.Component{Manifest: component.Manifest{
			Name: "weather",
			Env:  []string{"WEATHER_API_HOST"},
		}}

		It("sets a declared variable from the host's configuration", func() {
			got := env(declared, cmd, component.EnvOptions{
				Extra: map[string]string{"WEATHER_API_HOST": "localhost:9000"},
			})
			Expect(got).To(HaveKeyWithValue("WEATHER_API_HOST", "localhost:9000"))
		})

		It("forwards FFT_BASE_URL when the manifest asks for it", func() {
			asks := component.Component{Manifest: component.Manifest{
				Name: "emulator",
				Env:  []string{config.EnvBaseURL},
			}}

			// The one FFT_ variable a component may ask for, and the reason it can: the
			// emulator's emit command talks to a running emulator at the address the
			// emulator's own recipe exported. It says where, never who.
			got := env(asks, cmd, component.EnvOptions{})
			Expect(got).To(HaveKeyWithValue(config.EnvBaseURL, "https://real.tenant"))

			// Asking for it does not smuggle the rest of the session along with it.
			Expect(got).NotTo(HaveKey(config.EnvIDToken))
			Expect(got).NotTo(HaveKey(config.EnvPassword))
			Expect(got).NotTo(HaveKey(config.EnvFirebaseAPIKey))
		})

		It("does not forward FFT_BASE_URL to a component that did not ask", func() {
			got := env(declared, cmd, component.EnvOptions{})
			Expect(got).NotTo(HaveKey(config.EnvBaseURL))
		})

		It("ignores configuration the manifest did not declare", func() {
			// What a component consumes stays something it declared and `fft component
			// info` can print, rather than whatever the host felt like pushing at it.
			got := env(declared, cmd, component.EnvOptions{
				Extra: map[string]string{"SOMETHING_ELSE": "surprise"},
			})
			Expect(got).NotTo(HaveKey("SOMETHING_ELSE"))
		})
	})
})
