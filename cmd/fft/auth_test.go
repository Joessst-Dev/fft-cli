package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

var _ = Describe("fft auth status", func() {
	const (
		password     = "pw-must-not-appear"
		refreshToken = "refresh-must-not-appear"
		apiKey       = "AIzaSyExample"
	)

	var (
		c         *cli
		now       time.Time
		minted    int
		idToken   string
		statusDoc func() map[string]any
	)

	BeforeEach(func() {
		c = newCLI()
		now = time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
		c.deps.Clock = func() time.Time { return now }

		// Status is offline: a token source being built at all means it would sign in.
		minted = 0
		c.deps.NewTokenSource = func(config.Project, secrets.Store, func() time.Time, io.Writer) (auth.TokenSource, error) {
			minted++
			return auth.StaticTokenSource(testIDToken), nil
		}

		idToken = jwtWithExpiry(now.Add(2 * time.Hour))

		statusDoc = func() map[string]any {
			GinkgoHelper()
			Expect(c.run("auth", "status", "-o", "json")).To(Equal(exitcode.OK))
			var doc map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &doc)).To(Succeed())
			return doc
		}
	})

	cache := func(expiry string) {
		GinkgoHelper()
		Expect(c.secrets.Set(secrets.Key("staging", secrets.KindRefreshToken), refreshToken)).To(Succeed())
		Expect(c.secrets.Set(secrets.Key("staging", secrets.KindIDToken), idToken)).To(Succeed())
		Expect(c.secrets.Set(secrets.Key("staging", secrets.KindIDTokenExp), expiry)).To(Succeed())
	}

	When("the project has only just been added", func() {
		BeforeEach(func() {
			Expect(addStaging(c, password)).To(Equal(exitcode.OK))
		})

		It("reports a password to sign in with and no cached token", func() {
			doc := statusDoc()

			Expect(doc).To(HaveKeyWithValue("project", "staging"))
			Expect(doc).To(HaveKeyWithValue("email", "bot@ocff-acme-staging.com"))
			Expect(doc).To(HaveKeyWithValue("store", "memory"))
			Expect(doc).To(HaveKeyWithValue("signIn", "password"))
			Expect(doc).To(HaveKeyWithValue("hasPassword", true))
			Expect(doc).To(HaveKeyWithValue("hasRefreshToken", false))
			Expect(doc).To(HaveKeyWithValue("hasIdToken", false))
			Expect(doc).To(HaveKeyWithValue("token", "none"))
			Expect(doc).To(HaveKeyWithValue("expired", false))
			Expect(doc).NotTo(HaveKey("expiresAt"))
		})

		It("neither signs in nor builds anything that could", func() {
			Expect(c.run("auth", "status")).To(Equal(exitcode.OK))
			Expect(minted).To(BeZero())
		})

		It("renders a one-row table naming what is stored", func() {
			Expect(c.run("auth", "status")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("SIGN-IN"))
			Expect(c.out()).To(MatchRegexp(`staging\s+memory\s+password\s+password\s+none`))
		})
	})

	DescribeTable("classifies the cached token by how long it has left",
		func(left time.Duration, state string, expired bool, table string) {
			Expect(addStaging(c, password)).To(Equal(exitcode.OK))
			cache(now.Add(left).Format(time.RFC3339))

			doc := statusDoc()
			Expect(doc).To(HaveKeyWithValue("token", state))
			Expect(doc).To(HaveKeyWithValue("expired", expired))
			Expect(doc).To(HaveKeyWithValue("expiresAt", now.Add(left).Format(time.RFC3339)))
			Expect(doc).To(HaveKeyWithValue("hasRefreshToken", true))
			Expect(doc).To(HaveKeyWithValue("hasIdToken", true))

			Expect(c.run("auth", "status")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(table))
		},
		Entry("valid", 42*time.Minute, "valid", false, "valid (42m0s left)"),
		Entry("inside the refresh leeway", 3*time.Minute, "expiring", false, "expiring (3m0s left)"),
		Entry("expired", -10*time.Minute, "expired", true, "expired (10m0s ago)"),
	)

	It("trusts the stored expiry over the token's own claim, as the token cache does", func() {
		Expect(addStaging(c, password)).To(Equal(exitcode.OK))
		cache(now.Add(-time.Minute).Format(time.RFC3339))

		Expect(statusDoc()).To(HaveKeyWithValue("token", "expired"))
	})

	It("reports an id token whose expiry nothing records as unknown", func() {
		Expect(addStaging(c, password)).To(Equal(exitcode.OK))
		idToken = "opaque-token-must-not-appear"
		cache("not a time")

		doc := statusDoc()
		Expect(doc).To(HaveKeyWithValue("token", "unknown"))
		Expect(doc).NotTo(HaveKey("expiresAt"))
	})

	DescribeTable("never prints a stored secret",
		func(format ...string) {
			Expect(addStaging(c, password)).To(Equal(exitcode.OK))
			cache(now.Add(time.Hour).Format(time.RFC3339))

			Expect(c.run(append([]string{"auth", "status"}, format...)...)).To(Equal(exitcode.OK))

			for _, secret := range []string{password, refreshToken, idToken, apiKey} {
				Expect(c.out()).NotTo(ContainSubstring(secret))
				Expect(c.errOut()).NotTo(ContainSubstring(secret))
			}
		},
		Entry("as a table"),
		Entry("as JSON", "-o", "json"),
		Entry("as YAML", "-o", "yaml"),
	)

	It("keeps a project name edited into the config file by hand from steering the terminal", func() {
		const name = "evil\x1b]52;c;cGF5bG9hZA==\a\u202e\nforged"
		cfg := config.New()
		cfg.ActiveProject = name
		cfg.Upsert(config.Project{Name: name, BaseURL: "https://acme.api.fulfillmenttools.com", Email: "bot@example.com"})
		Expect(c.deps.Config.Save(cfg)).To(Succeed())

		Expect(c.run("auth", "status")).To(Equal(exitcode.OK), c.errOut())

		lines := strings.Split(strings.TrimRight(c.out(), "\n"), "\n")
		Expect(lines).To(HaveLen(2), "a newline in the name forged a row")
		Expect(lines[1]).To(HavePrefix("evil]52;c;cGF5bG9hZA== forged"))
		Expect(c.out()).NotTo(ContainSubstring("\x1b"))
		Expect(c.out()).NotTo(ContainSubstring("\u202e"))

		Expect(statusDoc()).To(HaveKeyWithValue("project", name), "JSON carries the name as it is")
	})

	When("no project is configured", func() {
		It("exits 3", func() {
			Expect(c.run("auth", "status")).To(Equal(exitcode.Config))
		})
	})

	When("fft is running from the environment", func() {
		var t *tenant

		BeforeEach(func() {
			t = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				w.WriteHeader(http.StatusInternalServerError)
			})
			// The harness's in-memory store stands in for the keychain; headless mode
			// reads the environment instead, and must be let to choose it.
			c.deps.Secrets = nil
		})

		It("reports the environment's password and sends nothing", func() {
			doc := statusDoc()

			Expect(doc).To(HaveKeyWithValue("project", config.EphemeralName))
			Expect(doc).To(HaveKeyWithValue("store", "env"))
			Expect(doc).To(HaveKeyWithValue("signIn", "password"))
			Expect(doc).To(HaveKeyWithValue("token", "none"))
			Expect(t.recorded()).To(BeEmpty())
			Expect(minted).To(BeZero())
		})

		When("it is given a fixed id token and no password", func() {
			BeforeEach(func() {
				unsetenv(config.EnvPassword)
				c.setenv(config.EnvIDToken, idToken)
			})

			It("reads the token's expiry from its own claim", func() {
				doc := statusDoc()

				Expect(doc).To(HaveKeyWithValue("signIn", "idToken"))
				Expect(doc).To(HaveKeyWithValue("hasPassword", false))
				Expect(doc).To(HaveKeyWithValue("token", "valid"))
				Expect(doc).To(HaveKeyWithValue("expiresAt", now.Add(2*time.Hour).Format(time.RFC3339)))
				Expect(c.out()).NotTo(ContainSubstring(idToken))
			})

			It("prefers FFT_ID_TOKEN_EXPIRES_AT when it is set", func() {
				c.setenv("FFT_ID_TOKEN_EXPIRES_AT", now.Add(-time.Second).Format(time.RFC3339))

				Expect(statusDoc()).To(HaveKeyWithValue("token", "expired"))
			})
		})
	})
})

// jwtWithExpiry builds an unsigned token whose only claim that matters is exp.
func jwtWithExpiry(exp time.Time) string {
	enc := base64.RawURLEncoding
	claims := fmt.Sprintf(`{"sub":"user-42","exp":%d}`, exp.Unix())
	return enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString([]byte(claims)) + ".sig"
}
