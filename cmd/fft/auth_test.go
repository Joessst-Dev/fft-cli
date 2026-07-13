package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// permissionsBody is what GET /api/users/me/effectivepermissions answers with.
const permissionsBody = `{
  "userId": "user-42",
  "roles": [
    {"name": "PICKER", "permissions": ["PICKJOB_READ", "PICKJOB_WRITE"]},
    {"name": "VIEWER", "permissions": ["FACILITY_READ"]}
  ]
}`

var _ = Describe("fft auth whoami", func() {
	var (
		c        *cli
		requests *[]*http.Request
	)

	BeforeEach(func() {
		c = newCLI()
		requests = c.fakeAPI(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(permissionsBody))
			Expect(err).NotTo(HaveOccurred())
		})
	})

	It("renders the roles and their permissions", func() {
		Expect(c.run("auth", "whoami")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring("PICKER"))
		Expect(c.out()).To(ContainSubstring("PICKJOB_READ, PICKJOB_WRITE"))
		Expect(c.out()).To(ContainSubstring("FACILITY_READ"))
	})

	It("presents the id token as a bearer credential", func() {
		Expect(c.run("auth", "whoami")).To(Equal(exitcode.OK))

		Expect(*requests).To(HaveLen(1))
		Expect((*requests)[0].URL.Path).To(Equal("/api/users/me/effectivepermissions"))
		Expect((*requests)[0].Header.Get("Authorization")).To(Equal("Bearer " + testIDToken))
	})

	It("sends no Firebase API key to the tenant", func() {
		// The key is Google's and identifies a Firebase project. A tenant has no
		// business receiving it, and the transport is built so that it cannot.
		Expect(c.run("auth", "whoami")).To(Equal(exitcode.OK))

		req := (*requests)[0]
		Expect(req.URL.Query()).NotTo(HaveKey("key"))
		Expect(req.Header.Get("X-Goog-Api-Key")).To(BeEmpty())
	})

	It("keeps the identity line on stderr, so -o json pipes cleanly into jq", func() {
		Expect(c.run("auth", "whoami", "-o", "json")).To(Equal(exitcode.OK))

		var view struct {
			UserID string `json:"userId"`
			Roles  []struct {
				Name        string   `json:"name"`
				Permissions []string `json:"permissions"`
			} `json:"roles"`
		}
		Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())
		Expect(view.UserID).To(Equal("user-42"))
		Expect(view.Roles).To(HaveLen(2))

		Expect(c.errOut()).To(ContainSubstring("Signed in as"))
	})

	When("the tenant rejects the token", func() {
		BeforeEach(func() {
			c.fakeAPI(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, err := w.Write([]byte(`[{"summary":"Unauthorized"}]`))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		It("exits 4 and tells the user how to sign in again", func() {
			code := c.run("auth", "whoami")

			Expect(code).To(Equal(exitcode.Auth))
			Expect(c.errOut()).To(ContainSubstring("Unauthorized"))
			Expect(c.errOut()).To(ContainSubstring("fft auth refresh"))
			Expect(c.out()).To(BeEmpty())
		})
	})

	When("the account lacks the permission", func() {
		BeforeEach(func() {
			c.fakeAPI(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, err := w.Write([]byte(`[{"summary":"Forbidden"}]`))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		It("exits 5, which is a different thing from a bad credential", func() {
			Expect(c.run("auth", "whoami")).To(Equal(exitcode.Forbidden))
		})
	})

	When("authentication itself cannot be completed", func() {
		BeforeEach(func() {
			c.deps.NewTokenSource = func(config.Project, secrets.Store, func() time.Time, io.Writer) (auth.TokenSource, error) {
				return nil, &auth.ReauthError{Project: "env", Err: context.DeadlineExceeded}
			}
		})

		It("exits 4 and names the command that fixes it", func() {
			code := c.run("auth", "whoami")

			Expect(code).To(Equal(exitcode.Auth))
			Expect(c.errOut()).To(ContainSubstring("fft project add env --force"))
		})
	})
})

var _ = Describe("fft auth token", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
		c.fakeAPI(func(http.ResponseWriter, *http.Request) {})
	})

	It("prints the id token bare, so it can be substituted into a curl command", func() {
		// The specs' stdout is a buffer, not a terminal, so this is the piped case:
		// $(fft auth token) needs no flag.
		Expect(c.run("auth", "token")).To(Equal(exitcode.OK))

		Expect(c.out()).To(Equal(testIDToken + "\n"))
		Expect(c.errOut()).To(BeEmpty())
	})

	It("prints it with --raw too", func() {
		Expect(c.run("auth", "token", "--raw")).To(Equal(exitcode.OK))

		Expect(c.out()).To(Equal(testIDToken + "\n"))
	})
})

var _ = Describe("fft auth refresh", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
		c.fakeAPI(func(http.ResponseWriter, *http.Request) {})
	})

	When("the token source can renew", func() {
		var expiry time.Time

		BeforeEach(func() {
			expiry = time.Date(2026, 7, 12, 13, 0, 0, 0, time.UTC)
			c.deps.Clock = func() time.Time { return expiry.Add(-time.Hour) }
			c.deps.NewTokenSource = func(config.Project, secrets.Store, func() time.Time, io.Writer) (auth.TokenSource, error) {
				return &fakeRenewer{token: auth.Token{
					ID:        "fresh-id-token",
					Refresh:   "fresh-refresh-token",
					ExpiresAt: expiry,
					Email:     "ci-bot@ocff-acme-staging.com",
				}}, nil
			}
		})

		It("reports when the new token expires", func() {
			Expect(c.run("auth", "refresh")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("2026-07-12T13:00:00Z"))
			Expect(c.out()).To(ContainSubstring("1h0m0s"))
		})

		It("never prints the token itself", func() {
			// `fft auth token --raw` is how a user asks for the token. A command whose
			// job is "prove the refresh worked" has no reason to leave one in the
			// scrollback.
			Expect(c.run("auth", "refresh")).To(Equal(exitcode.OK))

			Expect(c.out()).NotTo(ContainSubstring("fresh-id-token"))
			Expect(c.out()).NotTo(ContainSubstring("fresh-refresh-token"))
		})

		It("renders JSON without the token either", func() {
			Expect(c.run("auth", "refresh", "-o", "json")).To(Equal(exitcode.OK))

			Expect(c.out()).NotTo(ContainSubstring("fresh-id-token"))
			Expect(json.Valid([]byte(c.out()))).To(BeTrue())
		})
	})

	When("the project authenticates with a fixed id token", func() {
		It("exits 2: FFT_ID_TOKEN has nothing behind it to refresh from", func() {
			code := c.run("auth", "refresh")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("cannot be refreshed"))
		})
	})
})

// fakeRenewer is a TokenSource that can be renewed, standing in for the real
// Firebase one — whose own refresh path is covered against fake Google servers in
// internal/auth.
type fakeRenewer struct {
	token auth.Token
}

func (r *fakeRenewer) Token(context.Context) (string, error) { return r.token.ID, nil }

func (r *fakeRenewer) Renew(context.Context) (auth.Token, error) { return r.token, nil }
