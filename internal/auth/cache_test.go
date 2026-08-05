package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

var _ = Describe("the caching Firebase token source", func() {
	var (
		g     *google
		clk   *clock
		store *secrets.MemStore
		src   *FirebaseTokenSource
		ctx   context.Context
	)

	BeforeEach(func() {
		g, clk = newGoogle(), newClock()
		store = storeWithPassword(testPassword)
		src = NewFirebaseTokenSource(g.client(clk.Now), testProjectConfig(), store, clk.Now)
		ctx = context.Background()
	})

	When("nothing is cached yet", func() {
		It("signs in with the stored password", func() {
			token, err := src.Token(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("id-from-signin-1"))

			signIns, refreshes := g.counts()
			Expect(signIns).To(Equal(1))
			Expect(refreshes).To(BeZero())
		})

		It("caches the new token so the next process does not have to sign in again", func() {
			_, err := src.Token(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.Snapshot()).To(HaveKeyWithValue("fft:staging:idToken", "id-from-signin-1"))
			Expect(store.Snapshot()).To(HaveKeyWithValue("fft:staging:refreshToken", "refresh-from-signin-1"))
			Expect(store.Snapshot()).To(HaveKeyWithValue("fft:staging:idTokenExp",
				clk.Now().Add(time.Hour).UTC().Format(time.RFC3339)))
		})
	})

	When("a cached token still has plenty of life left", func() {
		BeforeEach(func() {
			_, err := src.Token(ctx)
			Expect(err).NotTo(HaveOccurred())
		})

		It("serves it without talking to Google at all", func() {
			clk.advance(50 * time.Minute) // 10 minutes left, twice the leeway

			token, err := src.Token(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("id-from-signin-1"))

			signIns, refreshes := g.counts()
			Expect(signIns).To(Equal(1))
			Expect(refreshes).To(BeZero())
		})
	})

	When("a cached token has less than five minutes left", func() {
		BeforeEach(func() {
			_, err := src.Token(ctx)
			Expect(err).NotTo(HaveOccurred())

			clk.advance(56 * time.Minute) // 4 minutes left, inside the leeway
		})

		It("refreshes it before use, rather than letting it expire mid-request", func() {
			token, err := src.Token(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("id-from-refresh-1"))

			signIns, refreshes := g.counts()
			Expect(signIns).To(Equal(1), "the password is not needed while the refresh token works")
			Expect(refreshes).To(Equal(1))
		})

		It("persists the rotated refresh token, not just the id token", func() {
			_, err := src.Token(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.Snapshot()).To(HaveKeyWithValue("fft:staging:refreshToken", "refresh-from-refresh-1"))
		})
	})

	When("the cached refresh token is dead", func() {
		BeforeEach(func() {
			// What a project looks like after a fortnight away: a stale id token, a
			// refresh token Google no longer honours, and the password still in the
			// keychain.
			Expect(store.Set(secrets.Key(testProject, secrets.KindIDToken), "stale")).To(Succeed())
			Expect(store.Set(secrets.Key(testProject, secrets.KindRefreshToken), "long-dead")).To(Succeed())
			Expect(store.Set(secrets.Key(testProject, secrets.KindIDTokenExp),
				clk.Now().Add(-time.Hour).Format(time.RFC3339))).To(Succeed())

			g.failRefresh(400, "TOKEN_EXPIRED")
		})

		It("falls back to a full password sign-in", func() {
			token, err := src.Token(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("id-from-signin-1"))

			signIns, refreshes := g.counts()
			Expect(refreshes).To(Equal(1), "the dead refresh token is tried first")
			Expect(signIns).To(Equal(1))
		})

		When("the password no longer works either", func() {
			BeforeEach(func() {
				g.failSignIn(400, "INVALID_LOGIN_CREDENTIALS")
			})

			It("asks the user to sign in again, rather than retrying forever", func() {
				_, err := src.Token(ctx)

				Expect(err).To(MatchError(ErrReauthRequired))
			})

			It("exits 4 with the command that fixes it", func() {
				_, err := src.Token(ctx)

				Expect(exitcode.FromError(err)).To(Equal(exitcode.Auth))

				var reauth *ReauthError
				Expect(errors.As(err, &reauth)).To(BeTrue())
				Expect(reauth.Hint()).To(ContainSubstring("fft project add staging --force"))
			})
		})
	})

	When("there is no password stored at all", func() {
		BeforeEach(func() {
			store = secrets.NewMem()
			src = NewFirebaseTokenSource(g.client(clk.Now), testProjectConfig(), store, clk.Now)
		})

		It("asks the user to sign in again", func() {
			_, err := src.Token(ctx)

			Expect(err).To(MatchError(ErrReauthRequired))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Auth))
		})
	})

	// The read side of the same story, and the one every already-configured
	// project hits: a user who copies their config onto a WSL box has a project
	// fft knows about and a keychain it cannot ask. That must arrive as the
	// machine problem it is — not as ErrReauthRequired, which would send them off
	// to re-enter a password that is not the problem.
	When("the keychain the password lives in is not there", func() {
		BeforeEach(func() {
			src = NewFirebaseTokenSource(g.client(clk.Now), testProjectConfig(), unreachableStore{}, clk.Now)
		})

		It("reports the missing keychain rather than demanding a fresh sign-in", func() {
			_, err := src.Token(ctx)

			Expect(err).To(MatchError(secrets.ErrKeyringUnavailable))
			Expect(err).NotTo(MatchError(ErrReauthRequired))
		})
	})

	When("Google is unreachable while refreshing", func() {
		BeforeEach(func() {
			Expect(store.Set(secrets.Key(testProject, secrets.KindRefreshToken), "still-good")).To(Succeed())
			g.failRefresh(503, "UNAVAILABLE")
		})

		It("does not send the password: a network failure is not a dead token", func() {
			_, err := src.Token(ctx)

			Expect(err).To(HaveOccurred())
			Expect(err).NotTo(MatchError(ErrReauthRequired))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Unavailable))

			signIns, _ := g.counts()
			Expect(signIns).To(BeZero())
		})
	})

	When("several commands ask for a token at once", func() {
		It("mints exactly one, rather than one sign-in per caller", func() {
			const callers = 20

			var wg sync.WaitGroup
			tokens := make([]string, callers)
			errs := make([]error, callers)

			wg.Add(callers)
			for i := range callers {
				go func() {
					defer wg.Done()
					defer GinkgoRecover()
					tokens[i], errs[i] = src.Token(ctx)
				}()
			}
			wg.Wait()

			for i := range callers {
				Expect(errs[i]).NotTo(HaveOccurred())
				Expect(tokens[i]).To(Equal("id-from-signin-1"))
			}

			signIns, refreshes := g.counts()
			Expect(signIns).To(Equal(1))
			Expect(refreshes).To(BeZero())
		})
	})

	Describe("fft auth refresh", func() {
		It("mints a new token even though the cached one is still good", func() {
			_, err := src.Token(ctx)
			Expect(err).NotTo(HaveOccurred())

			token, err := src.Renew(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(token.ID).To(Equal("id-from-refresh-1"))

			_, refreshes := g.counts()
			Expect(refreshes).To(Equal(1), "the cache must not short-circuit an explicit refresh")
		})

		It("carries the email forward, which the snake_case response does not carry", func() {
			_, err := src.Token(ctx)
			Expect(err).NotTo(HaveOccurred())

			token, err := src.Renew(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(token.Email).To(Equal(testEmail))
		})
	})

	When("the credential store is read-only, as it is in CI", func() {
		BeforeEach(func() {
			// A GitHub runner has nowhere durable to put a refreshed token. Signing in
			// once per process is correct there, and is not a failure to report.
			readOnly := secrets.NewEnv(func(name string) (string, bool) {
				if name == "FFT_PASSWORD" {
					return testPassword, true
				}
				return "", false
			})
			src = NewFirebaseTokenSource(g.client(clk.Now), testProjectConfig(), readOnly, clk.Now)
		})

		It("signs in and serves the token without failing on the write it cannot do", func() {
			token, err := src.Token(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("id-from-signin-1"))
		})
	})
})

var _ = Describe("a static token source", func() {
	It("serves the token it was given, for a CI job that signed in elsewhere", func() {
		token, err := StaticTokenSource("eyJhbGciOi").Token(context.Background())

		Expect(err).NotTo(HaveOccurred())
		Expect(token).To(Equal("eyJhbGciOi"))
	})

	It("cannot be renewed: there is nothing behind a fixed token to renew from", func() {
		_, ok := StaticTokenSource("eyJhbGciOi").(Renewer)

		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("saveToken", func() {
	It("writes the token secrets in a fixed order, with the expiry last", func() {
		// A partial failure part-way through must not leave a fresh expiry vouching
		// for a stale id token, so the expiry is committed last. Ranging a map would
		// make which pairing survives a crash a matter of hash order.
		rec := &orderRecordingStore{}
		tok := Token{ID: "id", Refresh: "ref", ExpiresAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

		Expect(saveToken(rec, "staging", tok)).To(Succeed())

		Expect(rec.sets).To(Equal([]string{
			secrets.Key("staging", secrets.KindRefreshToken),
			secrets.Key("staging", secrets.KindIDToken),
			secrets.Key("staging", secrets.KindIDTokenExp),
		}))
	})

	// Deliberately unlike ErrReadOnly, which is tolerated because a CI runner has
	// nowhere durable to put a token and signing in once per process is correct
	// there. An unreachable keychain is not that: the password this token was
	// minted from came out of the same store, so a store that answers a read and
	// then cannot take a write is broken, and the user should hear about it.
	It("reports an unreachable keychain rather than swallowing it", func() {
		err := saveToken(unreachableStore{}, "staging", Token{ID: "id", Refresh: "ref"})

		Expect(err).To(MatchError(secrets.ErrKeyringUnavailable))
	})
})

// unreachableStore is a machine with no keychain on it. Reads fail the same way
// writes do — the distinction that matters is against ErrNotFound, which would
// mean the keychain answered and held nothing.
type unreachableStore struct{}

func (unreachableStore) Get(string) (string, error) { return "", secrets.ErrKeyringUnavailable }
func (unreachableStore) Set(string, string) error   { return secrets.ErrKeyringUnavailable }
func (unreachableStore) Delete(string) error        { return nil }
func (unreachableStore) Kind() string               { return "keyring" }

// orderRecordingStore records the order its keys are written in, and nothing else.
type orderRecordingStore struct{ sets []string }

func (s *orderRecordingStore) Get(string) (string, error) { return "", secrets.ErrNotFound }
func (s *orderRecordingStore) Set(key, _ string) error    { s.sets = append(s.sets, key); return nil }
func (s *orderRecordingStore) Delete(string) error        { return nil }
func (s *orderRecordingStore) Kind() string               { return "recording" }

var _ = Describe("a token's freshness", func() {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

	DescribeTable("Fresh",
		func(token Token, want bool) {
			Expect(token.Fresh(now, Leeway)).To(Equal(want))
		},
		Entry("an hour left", Token{ID: "x", ExpiresAt: now.Add(time.Hour)}, true),
		Entry("six minutes left", Token{ID: "x", ExpiresAt: now.Add(6 * time.Minute)}, true),
		Entry("four minutes left, inside the leeway", Token{ID: "x", ExpiresAt: now.Add(4 * time.Minute)}, false),
		Entry("already expired", Token{ID: "x", ExpiresAt: now.Add(-time.Second)}, false),
		Entry("no token at all", Token{ExpiresAt: now.Add(time.Hour)}, false),
	)
})
