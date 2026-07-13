package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/testsupport"
	"github.com/Joessst-Dev/fft-cli/internal/update"
)

// The release this fft pretends to be, and the moment it pretends it is. A
// cache's age is then arithmetic, not a function of how long the suite took.
const testVersion = "v1.2.1"

var testNow = time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

const testNotice = "⚡ fft v1.3.0 is available (you have v1.2.1) — brew upgrade fft"

// github is a stand-in for api.github.com that counts what reached it. The count
// is the assertion: a fresh cache must make *no* request, and only a request
// that never happened can be proved by counting.
//
// The counter is atomic because the background check may still be in flight in
// its own goroutine while the spec reads it.
type github struct {
	url      string
	requests atomic.Int64
}

// fakeGitHub replaces api.github.com, and makes the command tree a v1.2.1
// release build with a terminal to print to — the two conditions without which
// the notice is suppressed and there would be nothing to test.
func (c *cli) fakeGitHub(handler http.HandlerFunc) *github {
	gh := &github{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gh.requests.Add(1)
		handler(w, r)
	}))
	DeferCleanup(srv.Close)
	gh.url = srv.URL

	c.asVersion(testVersion)
	c.deps.Terminal = ptr(true)

	c.updateCache = filepath.Join(GinkgoT().TempDir(), "fft", "update.json")
	c.deps.Update = update.New(testVersion, c.updateCache,
		update.WithURL(srv.URL),
		update.WithClock(func() time.Time { return testNow }),
	)

	// The background check outlives the command on purpose — in production the
	// process exits and takes the goroutine with it. A spec has no such luxury:
	// Ginkgo removes the temp directory above the moment the spec ends, and a
	// goroutine still writing the cache into it loses a race with that removal
	// ("directory not empty"). So the spec joins the goroutine it started, which
	// is the only reason Deps.updateDone exists.
	//
	// Registered *after* the temp directory, and therefore run before it: cleanups
	// are LIFO. Registering this any earlier — in newCLI, say — would let the
	// directory be removed out from under the goroutine, which is precisely the
	// flake it is here to prevent.
	DeferCleanup(c.awaitUpdateCheck)

	return gh
}

// awaitUpdateCheck waits for the background release check, if one was started.
func (c *cli) awaitUpdateCheck() {
	if c.deps.updateDone == nil {
		return
	}
	// Generously: the goroutine's own deadline is 1.5s, and it always honours it.
	Eventually(c.deps.updateDone, 5*time.Second).Should(BeClosed())
}

// asVersion is the ldflags-stamped version, for the duration of the spec. It is
// what tells fft whether it is a release build at all, and there is no other way
// in: production sets it at link time.
func (c *cli) asVersion(v string) {
	previous := buildinfo.Version
	buildinfo.Version = v
	DeferCleanup(func() { buildinfo.Version = previous })
}

// cachedRelease puts a cache file in place, as a previous run would have left it.
func (c *cli) cachedRelease(version string, age time.Duration) {
	GinkgoHelper()

	data, err := json.Marshal(update.State{
		CheckedAt:     testNow.Add(-age),
		LatestVersion: version,
		URL:           "https://github.com/Joessst-Dev/fft-cli/releases/tag/" + version,
	})
	Expect(err).NotTo(HaveOccurred())

	Expect(os.MkdirAll(filepath.Dir(c.updateCache), 0o700)).To(Succeed())
	Expect(os.WriteFile(c.updateCache, data, 0o600)).To(Succeed())
}

// cached is what the update check left behind, or an error while it has not left
// anything behind yet.
//
// It returns the error rather than asserting on it because the background check
// is polled with Eventually, and a failed Expect inside a polled function aborts
// the spec instead of trying again.
func (c *cli) cached() (update.State, error) {
	data, err := os.ReadFile(c.updateCache)
	if err != nil {
		return update.State{}, err
	}

	var s update.State
	if err := json.Unmarshal(data, &s); err != nil {
		return update.State{}, err
	}
	return s, nil
}

// configure writes a config file. A headless run never reads one, and
// settings.updateCheck lives nowhere else.
func (c *cli) configure(cfg *config.Config) {
	GinkgoHelper()
	Expect(config.NewStore(c.configPath).Save(cfg)).To(Succeed())
}

// latestRelease is GitHub answering that the newest release is v1.3.0.
func latestRelease(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, err := w.Write([]byte(`{"tag_name":"v1.3.0","html_url":"https://github.com/Joessst-Dev/fft-cli/releases/tag/v1.3.0"}`))
	Expect(err).NotTo(HaveOccurred())
}

// deletedOK is a tenant that accepts the delete. `fft facility delete` is the
// vehicle for the notice specs because it writes *nothing* to stdout: that is
// what lets a spec assert stdout is byte-empty, which is the contract.
func deletedOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

var _ = Describe("the update notice", func() {
	var (
		c  *cli
		gh *github
	)

	BeforeEach(func() {
		c = newCLI()
		c.fakeAPI(deletedOK)
		gh = c.fakeGitHub(latestRelease)
	})

	When("the cache was refreshed less than 24 hours ago", func() {
		BeforeEach(func() {
			c.cachedRelease("v1.3.0", 23*time.Hour)
		})

		It("says so on stderr, and leaves stdout byte-empty", func() {
			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring(testNotice))

			// The whole contract in one line: a notice on stdout would corrupt the
			// one stream a script is allowed to trust.
			Expect(c.out()).To(BeEmpty())
		})

		It("makes no request to GitHub at all", func() {
			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			Expect(gh.requests.Load()).To(BeZero())
		})

		It("says nothing when the cached release is the one already installed", func() {
			c.cachedRelease(testVersion, time.Hour)

			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			Expect(c.errOut()).NotTo(ContainSubstring("available"))
			Expect(gh.requests.Load()).To(BeZero())
		})

		It("says nothing when the cached release is older than the one installed", func() {
			c.cachedRelease("v1.2.0", time.Hour)

			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			Expect(c.errOut()).NotTo(ContainSubstring("available"))
		})
	})

	When("the cache is stale", func() {
		BeforeEach(func() {
			c.cachedRelease("v1.2.1", 25*time.Hour)
		})

		It("asks GitHub in the background and rewrites the cache", func() {
			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			// In the background: the command has already returned, so the answer is
			// awaited here rather than in the command, which is the point.
			Eventually(c.cached).Should(HaveField("LatestVersion", Equal("v1.3.0")))
			Expect(gh.requests.Load()).To(BeEquivalentTo(1))
		})
	})

	When("there is no cache at all", func() {
		It("asks GitHub, and writes the cache the next run will read", func() {
			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			Eventually(c.cached).Should(And(
				HaveField("LatestVersion", Equal("v1.3.0")),
				HaveField("CheckedAt", Equal(testNow)),
			))
		})

		It("writes that cache 0600 — nobody else's business which version you run", func() {
			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			// The background check writes the cache by rename, so the file appears
			// already carrying its final mode: waiting for it to exist is enough,
			// there is no window in which it exists and is still world-readable.
			Eventually(func() error {
				_, err := os.Stat(c.updateCache)
				return err
			}).Should(Succeed())
			testsupport.ExpectOwnerOnlyFile(c.updateCache)
		})
	})

	Describe("giving up silently", func() {
		DescribeTable("when GitHub cannot answer",
			func(handler http.HandlerFunc) {
				c = newCLI()
				c.fakeAPI(deletedOK)
				gh = c.fakeGitHub(handler)

				Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

				Expect(c.errOut()).NotTo(ContainSubstring("available"))
				Expect(c.errOut()).To(ContainSubstring("Deleted facility"))

				// Stamped even so: without it, a user with no network would ask GitHub
				// on every single invocation for the rest of time.
				Eventually(c.cached).Should(HaveField("CheckedAt", Equal(testNow)))
			},
			Entry("404, because there are no releases yet", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			})),
			Entry("403, the unauthenticated rate limit", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			})),
			Entry("a body that is not a release", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(`<html>`))
				Expect(err).NotTo(HaveOccurred())
			})),
		)
	})

	When("GitHub is slower than the command", func() {
		It("does not delay it: a user on a plane waits for nothing", func() {
			blocked := make(chan struct{})

			c = newCLI()
			c.fakeAPI(deletedOK)
			gh = c.fakeGitHub(func(_ http.ResponseWriter, r *http.Request) {
				select {
				case <-blocked:
				case <-r.Context().Done():
				}
			})
			// Released before the server is closed, which is the reverse of the order
			// they were registered in — LIFO, which is what makes this safe.
			DeferCleanup(func() { close(blocked) })

			start := time.Now()
			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))
			elapsed := time.Since(start)

			// The check has its own 1.5s deadline; the command must not have waited
			// anywhere near it. The result is simply dropped.
			Expect(elapsed).To(BeNumerically("<", update.Timeout))
			Expect(c.errOut()).NotTo(ContainSubstring("available"))
		})

		It("has already stamped the cache, so the next run does not ask again", func() {
			// The bug this pins: the process exits while the request is still in
			// flight — every command is faster than a round trip to GitHub — and the
			// goroutine dies with it, having written nothing. fft would then ask
			// GitHub on *every single invocation*, forever. Observed against the real
			// binary before the stamp was moved ahead of the request.
			blocked := make(chan struct{})

			c = newCLI()
			c.fakeAPI(deletedOK)
			c.fakeGitHub(func(_ http.ResponseWriter, r *http.Request) {
				select {
				case <-blocked:
				case <-r.Context().Done():
				}
			})
			DeferCleanup(func() { close(blocked) })

			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			// Synchronously: no Eventually, because nothing may be waited for. The
			// answer never arrived, and the stamp is there regardless.
			Expect(c.cached()).To(HaveField("CheckedAt", Equal(testNow)))
		})
	})

	Describe("when the notice must not be shown", func() {
		BeforeEach(func() {
			c.cachedRelease("v1.3.0", time.Hour)
		})

		// Each of these must suppress the *check* as well as the notice: a run whose
		// notice nobody would see has no business spending the machine's share of
		// GitHub's rate limit.
		It("stays quiet with -o json, where a machine is reading", func() {
			Expect(c.run("facility", "delete", "BER-01", "--yes", "-o", "json")).To(Equal(exitcode.OK))

			Expect(c.errOut()).NotTo(ContainSubstring("available"))
			Expect(gh.requests.Load()).To(BeZero())
		})

		It("stays quiet with -o yaml", func() {
			Expect(c.run("facility", "delete", "BER-01", "--yes", "-o", "yaml")).To(Equal(exitcode.OK))

			Expect(c.errOut()).NotTo(ContainSubstring("available"))
		})

		It("stays quiet when stderr is not a terminal — a pipe, a log file, a CI job", func() {
			c.deps.Terminal = ptr(false)

			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			Expect(c.errOut()).NotTo(ContainSubstring("available"))
			Expect(gh.requests.Load()).To(BeZero())
		})

		It("stays quiet when FFT_NO_UPDATE_CHECK is set", func() {
			c.setenv("FFT_NO_UPDATE_CHECK", "1")

			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			Expect(c.errOut()).NotTo(ContainSubstring("available"))
			Expect(gh.requests.Load()).To(BeZero())
		})

		It("stays quiet on a dev build, which has no version to compare", func() {
			c.asVersion("dev")

			Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

			Expect(c.errOut()).NotTo(ContainSubstring("available"))
			Expect(gh.requests.Load()).To(BeZero())
		})

		DescribeTable("on a command that is about versions already",
			func(args ...string) {
				Expect(c.run(args...)).To(Equal(exitcode.OK))

				Expect(c.errOut()).NotTo(ContainSubstring("available"))
			},
			Entry("fft version", "version"),
			Entry("fft completion", "completion", "bash"),
			// Every TAB keypress runs this one. Starting an HTTP request on each is
			// the single worst place in the tree to do it.
			Entry("the hidden completion command", "__complete", "facility", ""),
		)
	})

	Describe("a command that merely shares a name with fft update", func() {
		// The bug this pins: exempting every command with an *ancestor* named
		// "update" also exempts `fft facility update` and `fft stock update`. A user
		// whose daily driver is one of those would never be told about a release —
		// and their cache would never even be stamped, so fft would ask GitHub again
		// on the very next invocation.
		DescribeTable("is not exempt: it is a resource command, not a version command",
			func(entity, id, body string) {
				c = newCLI()
				c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
					writeJSON(w, http.StatusOK, body)
				})
				c.fakeGitHub(latestRelease)
				c.cachedRelease("v1.3.0", time.Hour)

				// --if-version skips the read-before-write. What is on trial here is the
				// command's *name*, not the shape of its requests.
				Expect(c.run(entity, "update", id,
					"--file", tempFile(body), "--if-version", "1")).To(Equal(exitcode.OK))

				Expect(c.errOut()).To(ContainSubstring(testNotice))
			},
			Entry("fft facility update", "facility", "BER-01",
				`{"id":"BER-01","name":"Berlin","type":"MANAGED_FACILITY","version":1}`),
			Entry("fft stock update", "stock", "STK-1",
				`{"id":"STK-1","tenantArticleId":"4711","value":20,"version":1}`),
		)
	})
})

// settings.updateCheck lives in the config file, and a headless run deliberately
// never reads one — so these specs are the one place the FFT_* variables must
// not be set, which is why they are not inside the Describe above.
var _ = Describe("settings.updateCheck in the config file", func() {
	var (
		c  *cli
		gh *github
	)

	// configured writes a config file with one project, and the given setting.
	configured := func(updateCheck bool) {
		cfg := config.New()
		cfg.Projects = []config.Project{{
			Name:           "staging",
			BaseURL:        "https://staging.api.fulfillmenttools.com",
			FirebaseAPIKey: "AIza",
			Email:          "bot@ocff-acme-staging.com",
		}}
		cfg.ActiveProject = "staging"
		cfg.Settings.UpdateCheck = updateCheck
		c.configure(cfg)
	}

	BeforeEach(func() {
		c = newCLI()
		gh = c.fakeGitHub(latestRelease)
		c.cachedRelease("v1.3.0", time.Hour)
	})

	It("shows the notice when it is true, which is what a fresh config says", func() {
		configured(true)

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring(testNotice))
	})

	It("stays quiet when it is false, and asks GitHub nothing", func() {
		configured(false)

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.errOut()).NotTo(ContainSubstring("available"))
		Expect(gh.requests.Load()).To(BeZero())
	})

	// The setting defaults to true and a missing file is not an error, so a first
	// run gets the notice. Both halves of that are load-bearing: were a missing
	// file to fail the load, updateAllowed would read the failure as "no", and
	// every user would be silently denied the notice until the day they first ran
	// `fft project add`. There is nothing to see when that breaks — which is why
	// it is pinned here rather than left to the two specs above, both of which
	// write a config file first.
	It("shows the notice on a fresh install, where there is no config file at all", func() {
		Expect(c.configPath).NotTo(BeAnExistingFile())

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring(testNotice))
	})
})

// The specs run in the developer's environment, or the CI runner's, and fft reads
// a great deal of it. This pins the harness that takes it away from them.
var _ = Describe("the spec harness", func() {
	// FFT_NO_UPDATE_CHECK is the one that bit: CI exports it for every job, so the
	// update check was switched off underneath its own specs — green locally, red
	// on all six matrix jobs. FFT_OUTPUT stands for the rest of the FFT_* surface
	// viper's AutomaticEnv exposes, any of which a developer may have exported.
	DescribeTable("clears the variables the machine exported, so the specs test fft and not the machine",
		func(name string) {
			GinkgoT().Setenv(name, "1")

			newCLI()

			_, set := os.LookupEnv(name)
			Expect(set).To(BeFalse(), "%s survived newCLI: the spec is reading the machine's environment", name)
		},
		Entry("FFT_NO_UPDATE_CHECK, which every CI job exports", "FFT_NO_UPDATE_CHECK"),
		Entry("FFT_OUTPUT, which viper reads as --output", "FFT_OUTPUT"),
		Entry("FFT_BASE_URL, which would synthesize a headless project", config.EnvBaseURL),
	)

	It("points every path fft resolves on its own at a directory of the spec's own", func() {
		newCLI()

		for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
			Expect(os.Getenv(name)).To(BeADirectory(), "%s must be the spec's, never the developer's", name)
		}
	})
})

var _ = Describe("fft update check", func() {
	var (
		c  *cli
		gh *github
	)

	BeforeEach(func() {
		c = newCLI()
		gh = c.fakeGitHub(latestRelease)
	})

	It("asks GitHub even when the cache is fresh: the user asked for an answer now", func() {
		c.cachedRelease(testVersion, time.Minute)

		Expect(c.run("update", "check")).To(Equal(exitcode.OK))

		Expect(gh.requests.Load()).To(BeEquivalentTo(1))

		// Synchronously, unlike the background check: the command waited for it.
		Expect(c.cached()).To(HaveField("LatestVersion", Equal("v1.3.0")))
	})

	It("renders the answer as data, and the upgrade path as advice on stderr", func() {
		Expect(c.run("update", "check")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring("CURRENT"))
		Expect(c.out()).To(ContainSubstring("v1.3.0"))
		Expect(c.out()).To(ContainSubstring("update available"))
		Expect(c.errOut()).To(ContainSubstring(testNotice))
	})

	It("emits the release as JSON on stdout, with nothing but data on it", func() {
		Expect(c.run("update", "check", "-o", "json")).To(Equal(exitcode.OK))

		var view struct {
			Current  string `json:"current"`
			Latest   string `json:"latest"`
			UpToDate bool   `json:"upToDate"`
			URL      string `json:"url"`
		}
		Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())

		Expect(view.Current).To(Equal(testVersion))
		Expect(view.Latest).To(Equal("v1.3.0"))
		Expect(view.UpToDate).To(BeFalse())
		Expect(view.URL).To(ContainSubstring("releases/tag/v1.3.0"))
	})

	It("reports that fft is up to date when it is", func() {
		c = newCLI()
		gh = c.fakeGitHub(func(w http.ResponseWriter, _ *http.Request) {
			_, err := w.Write([]byte(`{"tag_name":"v1.2.1"}`))
			Expect(err).NotTo(HaveOccurred())
		})

		Expect(c.run("update", "check")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring("up to date"))
		Expect(c.errOut()).NotTo(ContainSubstring("available"))
		Expect(gh.requests.Load()).To(BeEquivalentTo(1))
	})

	It("says the truth on a dev build: not 'up to date', which would be a lie", func() {
		// `go build` produces "dev", which has no place in the version ordering. The
		// command is deliberately exempt from the release-build gate — you may always
		// ask — but the answer must not be an invented one. A dev binary six releases
		// behind reporting "up to date" is exactly the answer a script would act on.
		c.asVersion("dev")
		c.deps.Update = update.New("dev", c.updateCache, update.WithURL(gh.url))

		Expect(c.run("update", "check")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring("unknown"))
		Expect(c.out()).NotTo(ContainSubstring("up to date"))
		Expect(c.errOut()).NotTo(ContainSubstring("available"))
	})

	It("reports upToDate as null on a dev build, because the answer is unknown", func() {
		c.asVersion("dev")
		c.deps.Update = update.New("dev", c.updateCache, update.WithURL(gh.url))

		Expect(c.run("update", "check", "-o", "json")).To(Equal(exitcode.OK))

		var view map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())
		Expect(view).To(HaveKeyWithValue("upToDate", BeNil()))
	})

	It("honours --timeout, rather than a deadline baked into the HTTP client", func() {
		// The bug this pins: a 1.5s timeout on the checker's own http.Client silently
		// overruled --timeout, so a user on a slow link had no way to wait longer.
		slow := make(chan struct{})
		DeferCleanup(func() { close(slow) })

		c = newCLI()
		c.fakeGitHub(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(2 * update.Timeout):
			case <-slow:
			case <-r.Context().Done():
				return
			}
			latestRelease(w, r)
		})

		Expect(c.run("update", "check", "--timeout", "20s")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring("v1.3.0"))
	})

	It("does not repeat the notice on stderr under -o json: it is already in the payload", func() {
		Expect(c.run("update", "check", "-o", "json")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(BeEmpty())
	})

	It("refuses a response too large to be a release, rather than truncating it", func() {
		c = newCLI()
		c.fakeGitHub(func(w http.ResponseWriter, _ *http.Request) {
			// The write is expected to fail: refusing the response means closing the
			// connection while this body is still going out, which the server sees as
			// a reset. Asserting it succeeded would fail the spec exactly when the
			// code under test does the right thing.
			_, _ = w.Write([]byte(`{"tag_name":"v1.3.0","padding":"` + strings.Repeat("A", 2<<20) + `"}`))
		})

		Expect(c.run("update", "check")).NotTo(Equal(exitcode.OK))
	})

	It("needs no project: it talks to GitHub, not to a tenant", func() {
		// No config file, no FFT_* variables — the state of a fresh install, where
		// every other command would exit 3.
		Expect(c.run("update", "check")).To(Equal(exitcode.OK))
	})

	It("fails, loudly, when GitHub cannot answer — unlike the background check", func() {
		c = newCLI()
		c.fakeGitHub(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})

		Expect(c.run("update", "check")).NotTo(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("403"))
		Expect(c.out()).To(BeEmpty())
	})
})
