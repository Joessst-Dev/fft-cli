package update_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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

				file, err := os.Stat(cachePath)
				Expect(err).NotTo(HaveOccurred())
				Expect(file.Mode().Perm()).To(Equal(os.FileMode(0o600)))

				dir, err := os.Stat(filepath.Dir(cachePath))
				Expect(err).NotTo(HaveOccurred())
				Expect(dir.Mode().Perm()).To(Equal(os.FileMode(0o700)))
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
