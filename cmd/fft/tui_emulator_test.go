package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// fakeEmulator starts a loopback server standing in for the local emulator, and
// points nothing at it: a spec says where the runs go itself.
func (c *cli) fakeEmulator() (baseURL string, t *tenant) {
	t = &tenant{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.record(call{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query()})
		answerJSON(w, emptyFacilities)
	}))
	DeferCleanup(srv.Close)
	return srv.URL, t
}

// listFacilities answers the configured tenant's facility list, so that a run which
// reached the wrong tenant still succeeds — and the spec then fails on *where* it
// went rather than on an unrelated error.
func listFacilities(w http.ResponseWriter, _ *http.Request) { answerJSON(w, emptyFacilities) }

var _ = Describe("the environment a TUI run is given", func() {
	It("hides the shell's own tenant from it, and lets its policy through", func() {
		// A shell that is itself pointed at a real tenant, with every identity
		// variable fft reads exported.
		GinkgoT().Setenv(config.EnvBaseURL, "https://acme.api.fulfillmenttools.com")
		GinkgoT().Setenv(config.EnvFirebaseAPIKey, "AIzaSyReal")
		GinkgoT().Setenv(config.EnvEmail, "bot@ocff-acme-prod.com")
		GinkgoT().Setenv(config.EnvPassword, "real-secret")
		GinkgoT().Setenv(config.EnvUsername, "someone")
		GinkgoT().Setenv(config.EnvTenant, "acme")
		GinkgoT().Setenv(config.EnvProjectID, "ocff-acme-prod")
		GinkgoT().Setenv(config.EnvEnvironment, "prod")
		// Policy, not identity.
		GinkgoT().Setenv(config.EnvReadOnly, "1")
		GinkgoT().Setenv(config.EnvHistory, "on")
		GinkgoT().Setenv(component.EnvRoot, "/tmp/components")
		GinkgoT().Setenv("XDG_DATA_HOME", "/tmp/data")

		lookup := maskedEnv(config.EmulatorEnv("http://localhost:8080"))

		p, ok, err := config.FromEnv(lookup)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(p.BaseURL).To(Equal("http://localhost:8080"))
		Expect(p.Email).To(Equal("dev@localhost"))
		Expect(p.FirebaseAPIKey).To(Equal("emulator"))
		Expect(p.Username).To(BeEmpty())
		Expect(p.Tenant).To(BeEmpty())
		Expect(p.ProjectID).To(BeEmpty())
		Expect(p.Environment).To(BeEmpty())

		// The credential is the recipe's fixed token, and the shell's password is not
		// reachable at all — which is what stops a sign-in as somebody real.
		_, hasPassword := lookup(config.EnvPassword)
		Expect(hasPassword).To(BeFalse())
		token, hasToken := lookup(config.EnvIDToken)
		Expect(hasToken).To(BeTrue())
		Expect(token).To(Equal("emulator-token"))

		for _, name := range []string{config.EnvReadOnly, config.EnvHistory, component.EnvRoot, "XDG_DATA_HOME"} {
			v, ok := lookup(name)
			Expect(ok).To(BeTrue(), "%s was hidden, and it says what fft may do rather than to whom", name)
			Expect(v).To(Equal(os.Getenv(name)))
		}
	})
})

var _ = Describe("a TUI session pointed at the emulator", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("sends its runs to the emulator, and nothing to the configured tenant", func() {
		base, e := c.fakeEmulator()
		t := c.configuredTenant(listFacilities)

		r := c.newRunner()
		r.SetProject("prod")
		r.SetEmulator(base)

		id := start(r, tui.Invocation{Args: []string{"facility", "list"}})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(res.Project).To(Equal(config.EphemeralName))
		Expect(e.recorded()).To(HaveLen(1))
		Expect(t.recorded()).To(BeEmpty(), "the configured tenant was sent a request")
	})

	It("signs in to nothing, whatever the shell was going to sign in as", func() {
		base, _ := c.fakeEmulator()
		// A shell headless against a real tenant, signing in with a password.
		c.headless()

		r := c.newRunner()
		r.SetEmulator(base)

		id := start(r, tui.Invocation{Args: []string{"auth", "status"}})
		res := awaitDone(r, id)[id]
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)

		var st struct{ Project, Store, SignIn string }
		Expect(json.Unmarshal(res.Stdout, &st)).To(Succeed())
		Expect(st.Project).To(Equal(config.EphemeralName))
		Expect(st.Store).To(Equal("env"))
		// Not "password": the shell's FFT_PASSWORD never reached the run, so there is
		// nothing to sign in with but the recipe's fixed token.
		Expect(st.SignIn).To(Equal("idToken"))
	})

	It("keeps the session's read-only floor across the switch", func() {
		base, e := c.fakeEmulator()
		c.setenv(config.EnvReadOnly, "1")

		r := c.newRunner()
		r.SetEmulator(base)

		id := start(r, tui.Invocation{Args: []string{"picking", "add-pick-job", "--data", `{"pickLineItems":[]}`}})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.ReadOnly), "stderr: %s", res.Stderr)
		Expect(e.recorded()).To(BeEmpty(), "a read-only session sent a write to the emulator")
	})

	It("goes on listing the configured projects, which are what it switches back to", func() {
		base, _ := c.fakeEmulator()
		c.configuredTenant(listFacilities)

		r := c.newRunner()
		r.SetEmulator(base)

		id := start(r, tui.Invocation{Args: []string{"project", "list"}})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(string(res.Stdout)).To(ContainSubstring(`"prod"`))
		Expect(string(res.Stdout)).To(ContainSubstring(`"other"`))
		Expect(string(res.Stdout)).NotTo(ContainSubstring(`"` + config.EphemeralName + `"`))
	})

	It("points the runs back at the selected project", func() {
		base, e := c.fakeEmulator()
		t := c.configuredTenant(listFacilities)

		r := c.newRunner()
		r.SetProject("prod")
		r.SetEmulator(base)
		r.SetEmulator("")

		id := start(r, tui.Invocation{Args: []string{"facility", "list"}})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(res.Project).To(Equal("prod"))
		Expect(t.recorded()).To(HaveLen(1))
		Expect(e.recorded()).To(BeEmpty())
	})

	It("leaves a run that was already started where it was started", func() {
		base, e := c.fakeEmulator()
		arrived, release := make(chan struct{}, 1), make(chan struct{})
		t := c.configuredTenant(func(w http.ResponseWriter, r *http.Request) {
			arrived <- struct{}{}
			<-release
			listFacilities(w, r)
		})

		r := c.newRunner()
		r.SetProject("prod")

		id := start(r, tui.Invocation{Args: []string{"facility", "list"}})
		Eventually(arrived).WithTimeout(runTimeout).Should(Receive())

		// The switch lands while the request is in flight. The user confirmed this run
		// against the project that was on screen when they started it.
		r.SetEmulator(base)
		close(release)

		res := awaitDone(r, id)[id]
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(res.Project).To(Equal("prod"))
		Expect(t.recorded()).To(HaveLen(1))
		Expect(e.recorded()).To(BeEmpty())
	})
})

var _ = Describe("a run the TUI is streaming", func() {
	It("does not hold the config file, so a switch behind a running server still happens", func() {
		c := newCLI()
		arrived, release := make(chan struct{}, 1), make(chan struct{})
		c.configuredTenant(func(w http.ResponseWriter, r *http.Request) {
			arrived <- struct{}{}
			<-release
			listFacilities(w, r)
		})
		DeferCleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})

		r := c.newRunner()
		// A streaming run that does not end until it is let go, standing in for the
		// emulator: `fft emulator` is a component, and a spec has none installed.
		held := start(r, tui.Invocation{Args: []string{"facility", "list"}, Stream: true})
		Eventually(arrived).WithTimeout(runTimeout).Should(Receive())

		// Exclusive, and it rewrites the config file. Before, this waited for the
		// server to be stopped — and Go's RWMutex would have queued every later run
		// behind it, so the whole UI would have stopped with it.
		id := start(r, tui.Invocation{Args: []string{"project", "use", "other"}, Exclusive: true})
		res := awaitDone(r, id)[id]
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(c.activeProject()).To(Equal("other"))

		close(release)
		awaitDone(r, held)
	})

	It("does not sweep a pre-v2 config file, which it holds no lock on", func() {
		c := newCLI()
		// A version-1 config with the Firebase API key still in the plaintext field,
		// as a pre-migration fft would have left it.
		Expect(os.MkdirAll(filepath.Dir(c.configPath), 0o700)).To(Succeed())
		Expect(os.WriteFile(c.configPath, []byte(`version: 1
activeProject: legacy
projects:
    - name: legacy
      baseUrl: https://legacy.api.fulfillmenttools.com
      firebaseApiKey: AIzaSyLegacy
      email: bot@ocff-acme-prd.com
`), 0o600)).To(Succeed())

		r := c.newRunner()
		id := start(r, tui.Invocation{Args: []string{"project", "list"}, Stream: true})
		Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))

		// The sweep is a read-modify-write, and this run takes no lock: doing it here
		// is how it loses an exclusive run's update.
		Expect(c.secrets.Snapshot()).NotTo(HaveKey("fft:legacy:apiKey"))
		data, err := os.ReadFile(c.configPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("firebaseApiKey"))

		// Deferred, not dropped: the next ordinary run holds the lock and does it.
		id = start(r, tui.Invocation{Args: []string{"project", "list"}})
		Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))
		Expect(c.secrets.Snapshot()).To(HaveKeyWithValue("fft:legacy:apiKey", "AIzaSyLegacy"))
	})
})

var _ = Describe("the commands a TUI session never redirects", func() {
	It("is the fft project group, and nothing else", func() {
		r := newCLIRunner(context.Background(), newCLI().deps, uiRun{})
		DeferCleanup(r.Close)

		var managed, free []string
		var walk func(cmd *cobra.Command, path []string)
		walk = func(cmd *cobra.Command, path []string) {
			if cmd.Runnable() {
				line := strings.Join(path, " ")
				if r.classify(path).managesProjects {
					managed = append(managed, line)
				} else {
					free = append(free, line)
				}
			}
			for _, sub := range cmd.Commands() {
				walk(sub, append(slices.Clone(path), sub.Name()))
			}
		}
		walk(r.catalog, nil)

		underProject := func(line string) bool { return line == "project" || strings.HasPrefix(line, "project ") }

		Expect(managed).NotTo(BeEmpty())
		for _, line := range managed {
			Expect(underProject(line)).To(BeTrue(),
				"%q is not under fft project, but a session pointed at the emulator would not redirect it", line)
		}
		for _, line := range free {
			Expect(underProject(line)).To(BeFalse(),
				"%q is under fft project, but would be redirected away from the config file", line)
		}
	})
})
