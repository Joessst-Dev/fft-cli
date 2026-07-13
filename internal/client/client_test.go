package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("decoding a fulfillmenttools error", func() {
	// The error envelope is an ARRAY — [{"summary":"…"}] — not an object. A struct
	// decodes it happily and produces {} for every error, which is how a precise
	// server message turns into silence on the user's terminal.
	It("reads the array envelope the API actually sends", func() {
		body := []byte(`[{"summary":"No facility matching request X was found!"}]`)

		err := client.Check(http.StatusNotFound, body)

		Expect(err).To(MatchError(ContainSubstring("No facility matching request X was found!")))
	})

	It("reads every error in the array, not just the first", func() {
		body := []byte(`[{"summary":"first"},{"summary":"second"}]`)

		err := client.Check(http.StatusBadRequest, body)

		Expect(err).To(MatchError(ContainSubstring("first")))
		Expect(err).To(MatchError(ContainSubstring("second")))
	})

	It("keeps the version numbers a 409 carries, for the message M4 will build from them", func() {
		body := []byte(`[{"summary":"stale","version":42,"requestVersion":41}]`)

		err := client.Check(http.StatusConflict, body)

		var apiErr *client.APIError
		Expect(errors.As(err, &apiErr)).To(BeTrue())
		Expect(apiErr.Errors).To(HaveLen(1))
		Expect(*apiErr.Errors[0].Version).To(BeEquivalentTo(42))
		Expect(*apiErr.Errors[0].RequestVersion).To(BeEquivalentTo(41))
	})

	When("the body is not the documented envelope at all", func() {
		It("quotes what did arrive, rather than reporting a bare status code", func() {
			err := client.Check(http.StatusBadGateway, []byte("<html>502 Bad Gateway</html>"))

			Expect(err).To(MatchError(ContainSubstring("502")))
			Expect(err).To(MatchError(ContainSubstring("Bad Gateway")))
		})
	})

	DescribeTable("classifying the failure for a script",
		func(status int, want int) {
			Expect(exitcode.FromError(client.Check(status, []byte(`[{"summary":"x"}]`)))).To(Equal(want))
		},
		Entry("401 is an authentication failure", http.StatusUnauthorized, exitcode.Auth),
		Entry("403 is a permission failure", http.StatusForbidden, exitcode.Forbidden),
		Entry("404 is a missing resource", http.StatusNotFound, exitcode.NotFound),
		Entry("409 is a version conflict", http.StatusConflict, exitcode.Conflict),
		Entry("500 is upstream being upstream", http.StatusInternalServerError, exitcode.Unavailable),
		Entry("400 is nothing more specific", http.StatusBadRequest, exitcode.General),
	)

	DescribeTable("a 2xx is not an error",
		func(status int) {
			Expect(client.Check(status, nil)).To(Succeed())
		},
		Entry("200 OK", http.StatusOK),
		Entry("201 Created", http.StatusCreated),
		Entry("204 No Content", http.StatusNoContent),
	)
})

var _ = Describe("reporting a request that never got an answer", func() {
	// http.Client wraps every transport failure in a *url.Error. When the cause is
	// fft's own — a token it could not mint — the `Get "https://…": ` prefix buries
	// the one sentence the user needs behind a URL they never typed.
	It("surfaces an authentication failure as itself, hint and all", func() {
		reauth := &auth.ReauthError{Project: "staging", Err: errors.New("the refresh token is dead")}
		wrapped := &url.Error{Op: "Get", URL: "https://acme.api.fulfillmenttools.com/api/facilities", Err: reauth}

		err := client.RequestError("list the facilities", wrapped)

		Expect(err).To(BeIdenticalTo(error(reauth)))
		Expect(err.Error()).NotTo(ContainSubstring("https://"))
		Expect(exitcode.FromError(err)).To(Equal(exitcode.Auth))
	})

	It("keeps the URL on a genuine network failure, where the URL is the point", func() {
		wrapped := &url.Error{
			Op:  "Get",
			URL: "https://acme.api.fulfillmenttools.com/api/status",
			Err: errors.New("dial tcp: no such host"),
		}

		err := client.RequestError("reach the tenant", wrapped)

		Expect(err).To(MatchError(ContainSubstring("reach the tenant")))
		Expect(err).To(MatchError(ContainSubstring("no such host")))
	})
})

var _ = Describe("building the API client", func() {
	var (
		tenant *httptest.Server
		got    *http.Request
	)

	BeforeEach(func() {
		tenant = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Clone(r.Context())
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"status":"OK"}`))
			Expect(err).NotTo(HaveOccurred())
		}))
		DeferCleanup(tenant.Close)
	})

	It("authenticates every request with the token source it was given", func() {
		c, err := client.New(tenant.URL, client.WithTokenSource(auth.StaticTokenSource("tok")))
		Expect(err).NotTo(HaveOccurred())

		res, err := c.API().StatusWithResponse(context.Background())

		Expect(err).NotTo(HaveOccurred())
		Expect(res.StatusCode()).To(Equal(http.StatusOK))
		Expect(got.Header.Get("Authorization")).To(Equal("Bearer tok"))
	})

	It("sends no Authorization header without one, which is what `fft ping` needs", func() {
		// GET /api/status is the only endpoint that answers without a token, so a
		// ping must be able to prove connectivity even when the credentials are
		// precisely what is broken.
		c, err := client.New(tenant.URL)
		Expect(err).NotTo(HaveOccurred())

		_, err = c.API().StatusWithResponse(context.Background())

		Expect(err).NotTo(HaveOccurred())
		Expect(got.Header).NotTo(HaveKey("Authorization"))
	})

	It("refuses a project with no base URL rather than requesting a relative path", func() {
		_, err := client.New("  ")

		Expect(err).To(MatchError(ContainSubstring("no base URL")))
	})
})
