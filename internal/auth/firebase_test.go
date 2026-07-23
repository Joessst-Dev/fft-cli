package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("signing in against Google Identity Platform", func() {
	var (
		g     *google
		clk   *clock
		c     *Client
		ctx   context.Context
		token Token
		err   error
	)

	BeforeEach(func() {
		g, clk = newGoogle(), newClock()
		c = g.client(clk.Now)
		ctx = context.Background()
	})

	When("the password is correct", func() {
		BeforeEach(func() {
			token, err = c.SignIn(ctx, testEmail, testPassword)
		})

		It("returns the id token from the camelCase response", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(token.ID).To(Equal("id-from-signin-1"))
			Expect(token.Refresh).To(Equal("refresh-from-signin-1"))
		})

		It("reports the email that actually authenticated", func() {
			Expect(token.Email).To(Equal(testEmail))
		})

		It("turns the expiresIn string into an absolute expiry", func() {
			// expiresIn arrives as the JSON *string* "3600". Declaring it an int
			// fails to unmarshal; the token would come back zero-valued.
			Expect(token.ExpiresAt).To(Equal(clk.Now().Add(time.Hour)))
		})

		It("sends the Firebase API key to Google, where it belongs", func() {
			Expect(g.receivedKeys()).To(ConsistOf(testAPIKey))
		})
	})

	When("the password is wrong", func() {
		BeforeEach(func() {
			token, err = c.SignIn(ctx, testEmail, "not-the-password")
		})

		It("says so in a sentence the user can act on", func() {
			Expect(err).To(MatchError(ContainSubstring("the email address or password is wrong")))
		})

		It("exits 4, so a script can tell a bad credential from a bad request", func() {
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Auth))
		})

		It("keeps the password and the API key out of the message", func() {
			Expect(err.Error()).NotTo(ContainSubstring("not-the-password"))
			Expect(err.Error()).NotTo(ContainSubstring(testAPIKey))
		})
	})

	When("Google is having an outage", func() {
		BeforeEach(func() {
			g.failSignIn(http.StatusInternalServerError, "INTERNAL")
			_, err = c.SignIn(ctx, testEmail, testPassword)
		})

		It("exits 9 (upstream unavailable), not 4: the credential is not the problem", func() {
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Unavailable))
		})

		It("is not a refusal, so nothing downstream may treat the credential as dead", func() {
			Expect(Refused(err)).To(BeFalse())
		})
	})

	DescribeTable("reporting a lifetime that is not a count of seconds",
		// Google has always answered with a string of digits. If that ever changes,
		// failing loudly here beats caching a token that expires at the zero time
		// and refreshing it before every single request.
		func(lifetime string) {
			g.setLifetime(lifetime)

			_, err := c.SignIn(ctx, testEmail, testPassword)

			Expect(err).To(MatchError(ContainSubstring("not a number of seconds")))
		},
		Entry("an empty string", ""),
		Entry("a word", "one hour"),
		Entry("zero", "0"),
	)
})

var _ = Describe("refreshing an id token", func() {
	var (
		g   *google
		clk *clock
		c   *Client
		ctx context.Context
	)

	BeforeEach(func() {
		g, clk = newGoogle(), newClock()
		c = g.client(clk.Now)
		ctx = context.Background()
	})

	It("parses the snake_case response, which is a different shape from sign-in", func() {
		// The securetoken endpoint answers id_token / refresh_token / expires_in,
		// where identitytoolkit answered idToken / refreshToken / expiresIn. One
		// struct for both decodes without error into an empty token — which then
		// fails as a 401 on the next request, a long way from the cause.
		token, err := c.Refresh(ctx, "refresh-from-signin-1")

		Expect(err).NotTo(HaveOccurred())
		Expect(token.ID).To(Equal("id-from-refresh-1"))
		Expect(token.Refresh).To(Equal("refresh-from-refresh-1"))
		Expect(token.ExpiresAt).To(Equal(clk.Now().Add(time.Hour)))
	})

	It("parses expires_in when it arrives as a JSON string", func() {
		g.setLifetime("1800")

		token, err := c.Refresh(ctx, "refresh-from-signin-1")

		Expect(err).NotTo(HaveOccurred())
		Expect(token.ExpiresAt).To(Equal(clk.Now().Add(30 * time.Minute)))
	})

	When("the refresh token is dead", func() {
		It("is a refusal, which is what licenses a fresh password sign-in", func() {
			g.failRefresh(http.StatusBadRequest, "TOKEN_EXPIRED")

			_, err := c.Refresh(ctx, "long-dead")

			Expect(Refused(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("the stored refresh token is no longer valid")))
		})
	})
})

var _ = Describe("the API key's blast radius", func() {
	// The Firebase Web API key belongs to Google, not to fulfillmenttools. The
	// guarantee is structural: the client that carries the key cannot reach any
	// host but Google's two, so no bug at a call site can leak it to a tenant.
	It("refuses to send a request to any host but Google's identity endpoints", func() {
		c, err := NewClient(testAPIKey)
		Expect(err).NotTo(HaveOccurred())

		_, err = c.hc.Get("https://acme.api.fulfillmenttools.com/api/facilities")

		Expect(err).To(MatchError(ContainSubstring("refusing to send the Firebase API key")))
		Expect(err).To(MatchError(ContainSubstring("acme.api.fulfillmenttools.com")))
	})

	It("allows exactly the two Google hosts and nothing else", func() {
		c, err := NewClient(testAPIKey)
		Expect(err).NotTo(HaveOccurred())

		transport, ok := c.hc.Transport.(*allowlistTransport)
		Expect(ok).To(BeTrue())
		Expect(transport.allowed).To(HaveLen(2))
		Expect(transport.allowed).To(HaveKey(hostOf(signInEndpoint)))
		Expect(transport.allowed).To(HaveKey(hostOf(refreshEndpoint)))
	})

	It("rejects an empty API key rather than asking Google to identify nobody", func() {
		_, err := NewClient("   ")

		Expect(err).To(MatchError(ContainSubstring("Firebase Web API key is empty")))
	})
})

var _ = Describe("redacting secrets from errors", func() {
	// Every error the standard library builds from an HTTP request quotes the URL
	// it failed on — and that URL carries ?key=. Without redaction the key lands in
	// the terminal, and from there in the issue the user pastes it into.
	It("removes the key parameter from a message", func() {
		msg := redactString(`Post "https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=AIzaSyREAL": dial tcp: timeout`)

		Expect(msg).NotTo(ContainSubstring("AIzaSyREAL"))
		Expect(msg).To(ContainSubstring("key=" + redacted))
	})

	It("removes a token or password quoted verbatim", func() {
		msg := redactString("refresh failed for token eyJhbGciOiJSUzI1NiJ9", "eyJhbGciOiJSUzI1NiJ9")

		Expect(msg).To(Equal("refresh failed for token " + redacted))
	})

	It("keeps the error chain intact, so errors.Is still works", func() {
		sentinel := errors.New("boom")

		err := redact(sentinel, testAPIKey)

		Expect(err).To(MatchError(sentinel))
	})

	It("leaves a nil error nil, so a call's result can be wrapped directly", func() {
		Expect(redact(nil, testAPIKey)).To(BeNil())
	})
})
