package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// mintingSource stands in for a Firebase token source: it mints a token the first
// time it is asked and every time it is renewed, and counts the mints in a counter
// shared by every source a spec builds.
type mintingSource struct {
	mints *atomic.Int64

	mu    sync.Mutex
	token string
}

func (s *mintingSource) Token(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" {
		s.mint()
	}
	return s.token, nil
}

func (s *mintingSource) Renew(context.Context) (auth.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mint()
	return auth.Token{ID: s.token}, nil
}

// mint must be called with mu held.
func (s *mintingSource) mint() {
	s.token = fmt.Sprintf("token-%d", s.mints.Add(1))
}

// tokenCounts are what a spec's token sources did: how many were built, and how
// many tokens they minted between them.
type tokenCounts struct {
	builds atomic.Int64
	mints  atomic.Int64
}

// countTokens replaces the spec's token sources with minting ones.
func (c *cli) countTokens() *tokenCounts {
	counts := &tokenCounts{}
	c.deps.NewTokenSource = func(config.Project, secrets.Store, func() time.Time, io.Writer) (auth.TokenSource, error) {
		counts.builds.Add(1)
		return &mintingSource{mints: &counts.mints}, nil
	}
	return counts
}

// bearers are the Authorization headers the tenant received, in order.
type bearers struct {
	mu   sync.Mutex
	seen []string
}

func (b *bearers) add(h string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen = append(b.seen, h)
}

func (b *bearers) all() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.seen...)
}

const emptyFacilities = `{"facilities":[],"total":0}`

func answerJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

var _ = Describe("the TUI session's tokens", func() {
	var (
		c      *cli
		counts *tokenCounts
		seen   *bearers
	)

	BeforeEach(func() {
		c = newCLI()
		counts = c.countTokens()
		seen = &bearers{}
	})

	runOK := func(r tui.Runner, args ...string) {
		GinkgoHelper()
		id := start(r, tui.Invocation{Args: args})
		res := awaitDone(r, id)[id]
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
	}

	When("the project's store keeps no token between runs", func() {
		BeforeEach(func() {
			// The environment's store is read-only: a minted token has nowhere to go.
			c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
				seen.add(r.Header.Get("Authorization"))
				answerJSON(w, emptyFacilities)
			})
		})

		It("signs in once for runs started side by side, and signs every one of them with that token", func() {
			r := c.newRunner()

			const runs = 3 * runnerSlots
			ids := make([]tui.RunID, runs)
			for i := range ids {
				ids[i] = start(r, tui.Invocation{Args: []string{"facility", "list"}})
			}
			for id, res := range awaitDone(r, ids...) {
				Expect(res.ExitCode).To(Equal(exitcode.OK), "run %d: %s", id, res.Stderr)
			}

			Expect(counts.mints.Load()).To(BeEquivalentTo(1))
			Expect(counts.builds.Load()).To(BeEquivalentTo(1))
			Expect(seen.all()).To(HaveLen(runs))
			Expect(seen.all()).To(HaveEach("Bearer token-1"))
		})

		It("signs in again for a command typed in a shell, which is a session of its own", func() {
			Expect(c.run("facility", "list")).To(Equal(exitcode.OK), c.errOut())
			Expect(c.run("facility", "list")).To(Equal(exitcode.OK), c.errOut())

			Expect(counts.mints.Load()).To(BeEquivalentTo(2))
		})

		It("builds a source of its own for a run that traces its requests", func() {
			r := c.newRunner()

			runOK(r, "facility", "list", "--debug")
			runOK(r, "facility", "list", "--debug")
			runOK(r, "facility", "list")

			Expect(counts.builds.Load()).To(BeEquivalentTo(3))
		})
	})

	It("renews the shared token after a 401, and the runs after it use the new one", func() {
		c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			bearer := r.Header.Get("Authorization")
			seen.add(bearer)
			if bearer == "Bearer token-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			answerJSON(w, emptyFacilities)
		})
		r := c.newRunner()

		runOK(r, "facility", "list")
		runOK(r, "facility", "list")

		Expect(seen.all()).To(Equal([]string{"Bearer token-1", "Bearer token-2", "Bearer token-2"}))
		Expect(counts.mints.Load()).To(BeEquivalentTo(2))
	})

	When("the projects come from the config file", func() {
		var r *cliRunner

		BeforeEach(func() {
			c.configuredTenant(func(w http.ResponseWriter, r *http.Request) {
				seen.add(r.Header.Get("Authorization"))
				answerJSON(w, emptyFacilities)
			})
			r = c.newRunner()
			r.SetProject("prod")
		})

		It("keeps one source per project for as long as nothing changes", func() {
			runOK(r, "facility", "list")
			runOK(r, "facility", "list")

			Expect(counts.builds.Load()).To(BeEquivalentTo(1))
		})

		It("forgets every token when the UI selects a project", func() {
			runOK(r, "facility", "list")
			r.SetProject("prod")
			runOK(r, "facility", "list")

			Expect(counts.builds.Load()).To(BeEquivalentTo(2))
			Expect(seen.all()).To(Equal([]string{"Bearer token-1", "Bearer token-2"}))
		})

		It("forgets every token after a command that rewrites the config file", func() {
			runOK(r, "facility", "list")
			runOK(r, "project", "use", "prod")
			runOK(r, "facility", "list")

			Expect(counts.builds.Load()).To(BeEquivalentTo(2))
		})

		It("does not hand a project changed outside the session the token of the account it was", func() {
			runOK(r, "facility", "list")

			cfg, err := c.deps.Config.Load()
			Expect(err).NotTo(HaveOccurred())
			p, err := cfg.Resolve("prod")
			Expect(err).NotTo(HaveOccurred())
			p.Email = "someone-else@ocff-acme-prod.com"
			cfg.Upsert(p)
			Expect(c.deps.Config.Save(cfg)).To(Succeed())

			runOK(r, "facility", "list")

			Expect(counts.builds.Load()).To(BeEquivalentTo(2))
			Expect(seen.all()).To(Equal([]string{"Bearer token-1", "Bearer token-2"}))
		})
	})
})
