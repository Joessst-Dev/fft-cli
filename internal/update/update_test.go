package update_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/testsupport"
	"github.com/Joessst-Dev/fft-cli/internal/update"
)

// now is the moment every spec pretends it is, so that a cache's age is decided
// by arithmetic rather than by how long the suite took to run.
var now = time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

// github is a stand-in for api.github.com that records what reached it.
type github struct {
	url      string
	requests []*http.Request
}

// fakeGitHub answers every request with handler, and counts them — the only way
// to prove that a fresh cache made no request *at all*.
func fakeGitHub(handler http.HandlerFunc) *github {
	g := &github{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.requests = append(g.requests, r.Clone(r.Context()))
		handler(w, r)
	}))
	DeferCleanup(srv.Close)

	g.url = srv.URL
	return g
}

// deadURL is a URL nothing is listening on: a server started and then stopped,
// so its port is real and its refusal is the one a laptop on a plane gets.
func deadURL() string {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	return srv.URL
}

// releaseJSON is GitHub's answer for a repository whose latest release is tag.
func releaseJSON(tag string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"tag_name":"` + tag + `","html_url":"https://github.com/Joessst-Dev/fft-cli/releases/tag/` + tag + `"}`))
		Expect(err).NotTo(HaveOccurred())
	}
}

// refusal is GitHub answering status with the given headers — the shape of a
// rate-limited answer, whose headers are the only thing that distinguishes it
// from a refusal of any other kind.
func refusal(status int, headers map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(status)
		// GitHub's own body names the IP address the budget belongs to. fft does not
		// repeat it: the user cannot act on it, and it is not fft's to publish.
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for 203.0.113.4."}`))
	}
}

// exhausted are the headers on a 403 whose hour is spent: 60 allowed, none left,
// and a reset 43 minutes after the moment every spec pretends it is.
func exhausted() map[string]string {
	return map[string]string{
		"X-RateLimit-Limit":     "60",
		"X-RateLimit-Remaining": "0",
		"X-RateLimit-Reset":     strconv.FormatInt(now.Add(43*time.Minute).Unix(), 10),
	}
}

var _ = Describe("update.Checker", func() {
	var (
		cachePath string
		clock     time.Time
	)

	BeforeEach(func() {
		cachePath = filepath.Join(GinkgoT().TempDir(), "fft", "update.json")
		clock = now
	})

	// checker builds the thing under test: the given fft version, the temp cache,
	// and a fake GitHub whose clock the spec controls.
	checker := func(version, url string) *update.Checker {
		return update.New(version, cachePath,
			update.WithURL(url),
			update.WithClock(func() time.Time { return clock }),
		)
	}

	// writeCache puts a cache file in place, as a previous run would have left it.
	writeCache := func(s update.State) {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(cachePath), 0o700)).To(Succeed())
		data, err := json.Marshal(s)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(cachePath, data, 0o600)).To(Succeed())
	}

	// readCache is what the checker left behind.
	readCache := func() update.State {
		GinkgoHelper()
		data, err := os.ReadFile(cachePath)
		Expect(err).NotTo(HaveOccurred(), "no cache file was written")

		var s update.State
		Expect(json.Unmarshal(data, &s)).To(Succeed())
		return s
	}

	Describe("Cached", func() {
		When("the cache was written less than 24 hours ago", func() {
			It("is fresh, so that no request is ever made", func() {
				writeCache(update.State{CheckedAt: now.Add(-23 * time.Hour), LatestVersion: "v1.3.0"})

				state, fresh := checker("v1.2.1", "http://127.0.0.1:1").Cached()

				Expect(fresh).To(BeTrue())
				Expect(state.LatestVersion).To(Equal("v1.3.0"))
			})
		})

		When("the cache is older than 24 hours", func() {
			It("is stale", func() {
				writeCache(update.State{CheckedAt: now.Add(-25 * time.Hour), LatestVersion: "v1.3.0"})

				_, fresh := checker("v1.2.1", "http://127.0.0.1:1").Cached()

				Expect(fresh).To(BeFalse())
			})
		})

		When("there is no cache file", func() {
			It("is not fresh, and reports no version", func() {
				state, fresh := checker("v1.2.1", "http://127.0.0.1:1").Cached()

				Expect(fresh).To(BeFalse())
				Expect(state).To(Equal(update.State{}))
			})
		})

		When("the cache file is corrupt", func() {
			It("is not fresh: a cache we cannot read is one we do not have", func() {
				Expect(os.MkdirAll(filepath.Dir(cachePath), 0o700)).To(Succeed())
				Expect(os.WriteFile(cachePath, []byte("{not json"), 0o600)).To(Succeed())

				_, fresh := checker("v1.2.1", "http://127.0.0.1:1").Cached()

				Expect(fresh).To(BeFalse())
			})
		})

		When("the cache was stamped in the future", func() {
			It("is not fresh: a clock that jumped must not silence the check forever", func() {
				writeCache(update.State{CheckedAt: now.Add(72 * time.Hour), LatestVersion: "v1.3.0"})

				_, fresh := checker("v1.2.1", "http://127.0.0.1:1").Cached()

				Expect(fresh).To(BeFalse())
			})
		})
	})

	Describe("Refresh", func() {
		When("GitHub answers with a release", func() {
			var gh *github

			BeforeEach(func() {
				gh = fakeGitHub(releaseJSON("v1.3.0"))
			})

			It("reports the release it was told about", func() {
				state, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(err).NotTo(HaveOccurred())
				Expect(state.LatestVersion).To(Equal("v1.3.0"))
				Expect(state.URL).To(ContainSubstring("releases/tag/v1.3.0"))
				Expect(state.CheckedAt).To(Equal(now))
			})

			It("rewrites the cache, so that the next run needs no request", func() {
				writeCache(update.State{CheckedAt: now.Add(-25 * time.Hour), LatestVersion: "v1.2.0"})

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())
				Expect(err).NotTo(HaveOccurred())

				Expect(readCache().LatestVersion).To(Equal("v1.3.0"))
				Expect(readCache().CheckedAt).To(Equal(now))
			})

			It("writes the cache 0600, in a 0700 directory", func() {
				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())
				Expect(err).NotTo(HaveOccurred())

				testsupport.ExpectOwnerOnlyFile(cachePath)
				testsupport.ExpectOwnerOnlyDir(filepath.Dir(cachePath))
			})

			It("leaves no temporary file behind: the write is a rename, not a truncate", func() {
				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())
				Expect(err).NotTo(HaveOccurred())

				entries, err := os.ReadDir(filepath.Dir(cachePath))
				Expect(err).NotTo(HaveOccurred())
				Expect(entries).To(HaveLen(1))
				Expect(entries[0].Name()).To(Equal("update.json"))
			})

			It("identifies itself to GitHub, which asks callers to", func() {
				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())
				Expect(err).NotTo(HaveOccurred())

				Expect(gh.requests).To(HaveLen(1))
				Expect(gh.requests[0].Method).To(Equal(http.MethodGet))
				Expect(gh.requests[0].Header.Get("User-Agent")).To(Equal("fft/v1.2.1"))
				Expect(gh.requests[0].Header.Get("Accept")).To(Equal("application/vnd.github+json"))
			})

			It("sends no credentials: the check is unauthenticated", func() {
				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())
				Expect(err).NotTo(HaveOccurred())

				Expect(gh.requests[0].Header).NotTo(HaveKey("Authorization"))
			})

			It("ignores a fresh cache — that is what makes it the forced check", func() {
				writeCache(update.State{CheckedAt: now, LatestVersion: "v1.2.1"})

				state, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(err).NotTo(HaveOccurred())
				Expect(state.LatestVersion).To(Equal("v1.3.0"))
				Expect(gh.requests).To(HaveLen(1))
			})
		})

		DescribeTable("when GitHub says no",
			func(status int, body string) {
				gh := fakeGitHub(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
					_, err := w.Write([]byte(body))
					Expect(err).NotTo(HaveOccurred())
				})

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(err).To(HaveOccurred())

				// The stamp is the whole point: without it, a repository with no
				// releases would be asked about on every single invocation.
				Expect(readCache().CheckedAt).To(Equal(now))
			},
			Entry("404, because there are no releases yet", http.StatusNotFound, `{"message":"Not Found"}`),
			Entry("403, the unauthenticated rate limit", http.StatusForbidden, `{"message":"API rate limit exceeded"}`),
			Entry("500", http.StatusInternalServerError, ``),
			Entry("200 with a body that is not JSON", http.StatusOK, `<html>`),
			Entry("200 with a release that has no tag", http.StatusOK, `{"html_url":"https://example.com"}`),
		)

		Describe("the unauthenticated rate limit", func() {
			// The bug this pins (#157): 403 is the status GitHub answers with when the
			// hour's budget is spent, and "403 Forbidden" reads as a permission fft
			// needs and does not have. It is nothing of the kind — the budget belongs
			// to the IP address, not to fft, and it refills on its own.
			It("names the limit and when it lifts, instead of 'Forbidden'", func() {
				gh := fakeGitHub(refusal(http.StatusForbidden, exhausted()))

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(err).To(MatchError(ContainSubstring("rate limit is exhausted")))
				Expect(err).To(MatchError(ContainSubstring("60 requests an hour")))
				Expect(err).To(MatchError(ContainSubstring("resets in 43m")))
				Expect(err).NotTo(MatchError(ContainSubstring("Forbidden")))
			})

			It("carries the hint that names whose budget it actually is", func() {
				gh := fakeGitHub(refusal(http.StatusForbidden, exhausted()))

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				var updateErr *update.Error
				Expect(errors.As(err, &updateErr)).To(BeTrue())
				Expect(updateErr.RateLimited()).To(BeTrue())
				Expect(updateErr.Limit).To(Equal(60))
				Expect(updateErr.Hint()).To(ContainSubstring("per IP address"))
				Expect(updateErr.Hint()).To(ContainSubstring("FFT_NO_UPDATE_CHECK=1"))
			})

			It("reads a 429 the same way: GitHub uses both for the same refusal", func() {
				gh := fakeGitHub(refusal(http.StatusTooManyRequests, exhausted()))

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(err).To(MatchError(ContainSubstring("rate limit is exhausted")))
			})

			It("leaves a 403 with requests still in hand as the refusal it is", func() {
				// The mistake in the other direction: GitHub really can say no for a
				// reason of its own, and calling that a rate limit would send the user
				// away to wait for a reset that is not coming.
				gh := fakeGitHub(refusal(http.StatusForbidden, map[string]string{
					"X-RateLimit-Limit":     "60",
					"X-RateLimit-Remaining": "57",
				}))

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(err).To(MatchError(ContainSubstring("403 Forbidden")))
				Expect(err).NotTo(MatchError(ContainSubstring("rate limit")))

				var updateErr *update.Error
				Expect(errors.As(err, &updateErr)).To(BeTrue())
				Expect(updateErr.RateLimited()).To(BeFalse())
				Expect(updateErr.Hint()).To(BeEmpty())
			})

			It("invents no reset time when GitHub named none", func() {
				gh := fakeGitHub(refusal(http.StatusForbidden, map[string]string{
					"X-RateLimit-Remaining": "0",
				}))

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(err).To(MatchError(ContainSubstring("rate limit is exhausted")))
				Expect(err).NotTo(MatchError(ContainSubstring("resets")))
				Expect(err).NotTo(MatchError(ContainSubstring("requests an hour")))
			})

			It("drops the countdown when the reset header is not a time", func() {
				headers := exhausted()
				headers["X-RateLimit-Reset"] = "soon"
				gh := fakeGitHub(refusal(http.StatusForbidden, headers))

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(err).To(MatchError(ContainSubstring("60 requests an hour")))
				Expect(err).NotTo(MatchError(ContainSubstring("resets")))
			})

			It("says 'under a minute' rather than counting seconds", func() {
				headers := exhausted()
				headers["X-RateLimit-Reset"] = strconv.FormatInt(now.Add(20*time.Second).Unix(), 10)
				gh := fakeGitHub(refusal(http.StatusForbidden, headers))

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(err).To(MatchError(ContainSubstring("resets in under a minute")))
			})

			It("stamps the cache, like every other answer that is not one", func() {
				gh := fakeGitHub(refusal(http.StatusForbidden, exhausted()))

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())
				Expect(err).To(HaveOccurred())

				Expect(readCache().CheckedAt).To(Equal(now))
			})
		})

		Describe("what a failed check exits with", func() {
			// Exit 9 across the board, because every one of these means the same thing
			// to whatever is reading it: fft is fine, GitHub had no answer, ask later.
			// Exit 1 told a script nothing at all.
			It("is Unavailable for a status GitHub refused with", func() {
				gh := fakeGitHub(refusal(http.StatusForbidden, exhausted()))

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(exitcode.FromError(err)).To(Equal(exitcode.Unavailable))
			})

			It("is Unavailable when there is no network at all", func() {
				_, err := checker("v1.2.1", deadURL()).Refresh(context.Background())

				Expect(exitcode.FromError(err)).To(Equal(exitcode.Unavailable))
			})

			It("is Unavailable when the answer is not a release", func() {
				gh := fakeGitHub(func(w http.ResponseWriter, _ *http.Request) {
					_, err := w.Write([]byte(`<html>`))
					Expect(err).NotTo(HaveOccurred())
				})

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())

				Expect(exitcode.FromError(err)).To(Equal(exitcode.Unavailable))
			})

			It("is Interrupted when the user pressed Ctrl-C", func() {
				// The regression guard on Unwrap: exitcode.FromError asks about
				// context.Canceled before it asks the error for its own code, and a
				// wrapper that hid the cause would turn every Ctrl-C into an exit 9.
				gh := fakeGitHub(releaseJSON("v1.3.0"))

				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := checker("v1.2.1", gh.url).Refresh(ctx)

				Expect(exitcode.FromError(err)).To(Equal(exitcode.Interrupted))
			})
		})

		When("the network is not there", func() {
			It("fails, and still stamps the cache so that we do not ask again today", func() {
				_, err := checker("v1.2.1", deadURL()).Refresh(context.Background())

				Expect(err).To(HaveOccurred())
				Expect(readCache().CheckedAt).To(Equal(now))
			})
		})

		When("a previous check knew of a release and this one fails", func() {
			It("keeps the release it knew about: old information is not wrong information", func() {
				writeCache(update.State{
					CheckedAt:     now.Add(-25 * time.Hour),
					LatestVersion: "v1.3.0",
					URL:           "https://example.com/v1.3.0",
				})

				gh := fakeGitHub(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})

				_, err := checker("v1.2.1", gh.url).Refresh(context.Background())
				Expect(err).To(HaveOccurred())

				Expect(readCache().LatestVersion).To(Equal("v1.3.0"))
				Expect(readCache().URL).To(Equal("https://example.com/v1.3.0"))
				Expect(readCache().CheckedAt).To(Equal(now))
			})
		})

		When("GitHub is slower than the caller allows", func() {
			It("gives up when the context does", func() {
				gh := fakeGitHub(func(w http.ResponseWriter, r *http.Request) {
					<-r.Context().Done()
				})

				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				DeferCleanup(cancel)

				done := make(chan error, 1)
				go func() {
					_, err := checker("v1.2.1", gh.url).Refresh(ctx)
					done <- err
				}()

				Eventually(done, time.Second).Should(Receive(HaveOccurred()))
			})
		})
	})

	Describe("Notice", func() {
		DescribeTable("what it tells the user",
			func(current, latest, expected string) {
				c := checker(current, "http://127.0.0.1:1")

				Expect(c.Notice(update.State{LatestVersion: latest})).To(Equal(expected))
			},
			Entry("a newer release is out",
				"v1.2.1", "v1.3.0",
				"⚡ fft v1.3.0 is available (you have v1.2.1) — brew upgrade fft"),
			Entry("a tag without the v prefix still names the upgrade path",
				"v1.2.1", "1.3.0",
				"⚡ fft v1.3.0 is available (you have v1.2.1) — brew upgrade fft"),
			Entry("nothing at all when the versions are equal", "v1.2.1", "v1.2.1", ""),
			Entry("nothing at all when the release is older", "v1.3.0", "v1.2.1", ""),
			Entry("nothing at all on a dev build", "dev", "v1.3.0", ""),
			Entry("nothing at all when nothing is known yet", "v1.2.1", "", ""),
		)
	})

	Describe("Newer", func() {
		It("compares versions numerically: v1.10.0 is newer than v1.9.0", func() {
			// The bug this exists to prevent: "v1.10.0" < "v1.9.0" as *strings*,
			// because '1' sorts before '9'. A string comparison would tell a user on
			// v1.10.0 to downgrade, and would never tell a user on v1.9.0 to upgrade.
			Expect("v1.10.0" < "v1.9.0").To(BeTrue(), "the premise of this spec")

			Expect(update.Newer("v1.9.0", "v1.10.0")).To(BeTrue())
			Expect(update.Newer("v1.10.0", "v1.9.0")).To(BeFalse())
		})

		DescribeTable("whether latest is newer than current",
			func(current, latest string, expected bool) {
				Expect(update.Newer(current, latest)).To(Equal(expected))
			},
			Entry("a newer patch", "v1.2.1", "v1.2.2", true),
			Entry("a newer minor", "v1.2.1", "v1.3.0", true),
			Entry("a newer major", "v1.9.9", "v2.0.0", true),
			Entry("the same version", "v1.2.1", "v1.2.1", false),
			Entry("an older release", "v1.2.1", "v1.2.0", false),
			Entry("the v prefix is optional on either side", "1.2.1", "1.3.0", true),
			Entry("a release beats the prerelease of the same version", "v1.3.0-rc1", "v1.3.0", true),
			Entry("a dev build compares to nothing", "dev", "v1.3.0", false),
			Entry("an empty current version compares to nothing", "", "v1.3.0", false),
			Entry("an unparseable tag compares to nothing", "v1.2.1", "latest", false),
		)
	})

	Describe("DefaultCachePath", func() {
		It("honours XDG_CACHE_HOME", func() {
			GinkgoT().Setenv("XDG_CACHE_HOME", "/tmp/xdg")

			Expect(update.DefaultCachePath()).To(Equal(filepath.Join("/tmp/xdg", "fft", "update.json")))
		})

		It("falls back to ~/.cache/fft/update.json", func() {
			GinkgoT().Setenv("XDG_CACHE_HOME", "")

			home, err := os.UserHomeDir()
			Expect(err).NotTo(HaveOccurred())

			Expect(update.DefaultCachePath()).To(Equal(filepath.Join(home, ".cache", "fft", "update.json")))
		})
	})
})
