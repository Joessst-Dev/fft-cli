package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

func TestFFT(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cmd/fft")
}

// testIDToken is what the faked token source mints. The specs assert on it to
// prove a command authenticated at all.
const testIDToken = "test-id-token"

// cli drives the real command tree against fakes: a config file in a temp
// directory, an in-memory keychain, and buffers in place of the terminal's
// streams. Every spec asserts on what a user would see — stdout, stderr and the
// exit code — rather than on the structs underneath.
type cli struct {
	deps    *Deps
	stdin   *bytes.Buffer
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	secrets *secrets.MemStore

	// configPath is where the config file would be written. A spec that must
	// prove nothing was written asserts that it still does not exist.
	configPath string

	// updateCache is where the release check would cache its answer. See
	// update_test.go, which is the only place that puts anything there.
	updateCache string
}

func newCLI() *cli {
	c := &cli{
		stdin:      &bytes.Buffer{},
		secrets:    secrets.NewMem(),
		configPath: filepath.Join(GinkgoT().TempDir(), "fft", "config.yaml"),
	}

	c.deps = &Deps{
		Config:  config.NewStore(c.configPath),
		Secrets: c.secrets,
		In:      c.stdin,

		// The retry policy is the real one but for the sleep, which returns at once:
		// a spec that proves a 502 is retried should cost microseconds, not the
		// several hundred milliseconds of real backoff. The *decisions* — how many
		// attempts, which methods — are still the production ones.
		Retry: client.Retry{
			Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		},

		// A spec must never talk to Google. These two seams are what the real
		// Firebase sign-in plugs into, so replacing them here replaces every
		// outbound identity request in the whole command tree — and a spec that
		// cares about verification (see project_test.go) overrides them again.
		Verify: func(_ context.Context, p config.Project, _ string, _ io.Writer) (string, error) {
			return p.Email, nil
		},
		NewTokenSource: func(config.Project, secrets.Store, func() time.Time, io.Writer) (auth.TokenSource, error) {
			return auth.StaticTokenSource(testIDToken), nil
		},
	}

	hermeticEnv()

	return c
}

// hermeticEnv gives the spec an environment fft has never seen before.
//
// The environment is process-global, and fft reads a lot of it: every FFT_*
// variable through viper's AutomaticEnv, XDG_* for the config, cache and state
// files, NO_COLOR for the output. Whatever of that the developer has exported —
// or the CI runner has — is not the spec's, and a spec that inherits it is a spec
// whose result depends on the machine it runs on.
//
// This is not hypothetical. CI exports FFT_NO_UPDATE_CHECK=1 for every job, and
// [Deps.updateAllowed] honours it: the entire update-notice feature was switched
// off underneath its own specs, which then failed on all six matrix jobs and on
// no developer's machine. Clearing one variable would have fixed that one spec.
// Clearing all of them is what stops the next one.
//
// A spec that wants a variable set says so — see [cli.setenv] and [cli.headless].
func hermeticEnv() {
	GinkgoHelper()

	// Empty, not unset: an empty NO_COLOR means colour is allowed, which is what
	// the specs assert against. The FFT_* variables below must be genuinely absent,
	// because config.FromEnv asks whether they are *set*, not whether they are
	// non-empty, and a half-empty headless project is worse than none.
	GinkgoT().Setenv("NO_COLOR", "")

	// Every path fft resolves on its own is a path inside this spec's temp
	// directory. No spec may read, or write, the developer's real config, cached
	// release check, or token file — XDG first, since that is what fft consults
	// first, and HOME behind it for the fallback that follows.
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		GinkgoT().Setenv(name, GinkgoT().TempDir())
	}

	for _, entry := range os.Environ() {
		if name, _, ok := strings.Cut(entry, "="); ok && strings.HasPrefix(name, "FFT_") {
			unsetenv(name)
		}
	}
}

// unsetenv removes a variable for the duration of the spec, and puts it back
// afterwards. Ginkgo's Setenv can only ever set one, and "" is a value: fft asks
// whether FFT_BASE_URL is *set*, so an empty one would synthesize a headless
// project pointing at nowhere.
func unsetenv(name string) {
	GinkgoHelper()

	previous, existed := os.LookupEnv(name)
	Expect(os.Unsetenv(name)).To(Succeed())

	DeferCleanup(func() {
		if !existed {
			return
		}
		Expect(os.Setenv(name, previous)).To(Succeed())
	})
}

// setenv sets an environment variable for the duration of the spec.
func (c *cli) setenv(name, value string) {
	GinkgoT().Setenv(name, value)
}

// answer makes the command tree believe it is on a terminal, and queues what the
// user types at it.
//
// Without this a spec can only ever prove that a destructive command *refuses*
// when it cannot ask — never that the question it asks says the right thing, and
// never that "n" actually stops it. Both are worth pinning: a confirmation that
// names the wrong facility, or that proceeds on a no, is a bug that only shows up
// the one time it matters.
func (c *cli) answer(lines ...string) {
	for _, line := range lines {
		_, err := c.stdin.WriteString(line + "\n")
		Expect(err).NotTo(HaveOccurred())
	}
	c.deps.Prompt = prompt.New(c.stdin, &c.stderr, prompt.WithInteractive(true))
}

// headless exports the FFT_* variables that make fft synthesize an ephemeral
// project — the CI path, where there is no keychain to talk to.
func (c *cli) headless() {
	c.setenv(config.EnvBaseURL, "https://ci.api.fulfillmenttools.com")
	c.setenv(config.EnvFirebaseAPIKey, "AIzaSyCI")
	c.setenv(config.EnvEmail, "ci-bot@ocff-acme-staging.com")
	c.setenv(config.EnvPassword, "ci-secret")
}

// fakeAPI starts a stand-in fulfillmenttools tenant and makes it the project the
// commands act on. Requests that reach it are recorded, so a spec can assert on
// what actually went over the wire.
func (c *cli) fakeAPI(handler http.HandlerFunc) *[]*http.Request {
	var seen []*http.Request

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(r.Context()))
		handler(w, r)
	}))
	DeferCleanup(srv.Close)

	c.headless()
	c.setenv(config.EnvBaseURL, srv.URL)

	return &seen
}

// call is one request the fake tenant received, body and all.
type call struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte

	// RawQuery is the query string as it went over the wire. Query has already
	// parsed it, and parsing is exactly what hides the difference between
	// status=A,B and status=A&status=B — the two encodings the spec assigns
	// per-parameter, and the one thing about a filter that is wrong *silently*.
	RawQuery string
}

// tenant is a stand-in fulfillmenttools tenant that records what reached it.
//
// It differs from [cli.fakeAPI] in keeping each request's *body*: a spec that
// must prove fft sent the version it read, or the URN rather than the raw id,
// can only do so by reading what went over the wire.
type tenant struct {
	calls []call
}

// fakeTenant starts the tenant and points the commands at it. handle answers each
// request; body is what the request carried.
func (c *cli) fakeTenant(handle func(w http.ResponseWriter, r *http.Request, body []byte)) *tenant {
	t := &tenant{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		Expect(err).NotTo(HaveOccurred())

		t.calls = append(t.calls, call{
			Method:   r.Method,
			Path:     r.URL.Path,
			Query:    r.URL.Query(),
			RawQuery: r.URL.RawQuery,
			Body:     body,
		})
		handle(w, r, body)
	}))
	DeferCleanup(srv.Close)

	c.headless()
	c.setenv(config.EnvBaseURL, srv.URL)

	return t
}

// only asserts that exactly one request was made, and returns it.
func (t *tenant) only() call {
	GinkgoHelper()
	Expect(t.calls).To(HaveLen(1))
	return t.calls[0]
}

// json decodes a recorded request body.
func (c call) json() map[string]any {
	GinkgoHelper()

	var doc map[string]any
	Expect(json.Unmarshal(c.Body, &doc)).To(Succeed(), "request body was not a JSON object: %s", c.Body)
	return doc
}

// run executes one command and returns its exit code. Each run gets a fresh
// command tree, because cobra's flag values are per-tree state.
func (c *cli) run(args ...string) int {
	c.stdout.Reset()
	c.stderr.Reset()

	// The cached config is dropped between runs so that a second command in the
	// same spec reads what the first one wrote, exactly as a second process would.
	c.deps.cfg = nil

	cmd := newRootCmd(c.deps)
	cmd.SetArgs(args)
	cmd.SetIn(c.stdin)
	cmd.SetOut(&c.stdout)
	cmd.SetErr(&c.stderr)

	err := cmd.ExecuteContext(context.Background())
	if err != nil {
		// main writes the error to stderr; the specs assert on the message, so the
		// harness has to do the same thing main does.
		writeError(&c.stderr, err)
	}
	return exitcode.FromError(err)
}

// out is everything the command wrote to stdout: the data a pipe would receive.
func (c *cli) out() string { return c.stdout.String() }

// errOut is everything the command wrote to stderr: prompts, warnings, notices.
func (c *cli) errOut() string { return c.stderr.String() }
