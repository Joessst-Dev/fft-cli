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

	Describe("an auth-class refusal from something between fft and the tenant", func() {
		DescribeTable("is reported as the intermediary's, not the tenant's",
			func(status int, body string) {
				err := client.Check(status, []byte(body))
				Expect(err).To(MatchError(ContainSubstring("a proxy or gateway between fft and the tenant refused the request")))
				Expect(err).To(MatchError(ContainSubstring("Approval required")))

				var coded interface{ Hint() string }
				Expect(errors.As(err, &coded)).To(BeTrue())
				Expect(coded.Hint()).NotTo(ContainSubstring("fft auth"))
			},
			Entry("403 with a plain-text body", http.StatusForbidden, "Approval required for example.test:443."),
			Entry("403 with an HTML body", http.StatusForbidden, "<html>Approval required</html>"),
			Entry("401 with a plain-text body", http.StatusUnauthorized, "Approval required"),
			Entry("407 whatever the body", http.StatusProxyAuthRequired, "Approval required"),
		)

		It("keeps the exit code, since scripts key on it", func() {
			Expect(exitcode.FromError(client.Check(http.StatusForbidden, []byte("Approval required")))).To(Equal(exitcode.Forbidden))
			Expect(exitcode.FromError(client.Check(http.StatusUnauthorized, []byte("Approval required")))).To(Equal(exitcode.Auth))
		})

		It("still blames the tenant when its own envelope is present", func() {
			err := client.Check(http.StatusForbidden, []byte(`[{"summary":"nope"}]`))
			Expect(err).To(MatchError("the API returned HTTP 403: nope"))
			Expect(err.(interface{ Hint() string }).Hint()).To(ContainSubstring("fft auth whoami"))
		})

		It("still blames the tenant for an empty array, which is its envelope's shape too", func() {
			err := client.Check(http.StatusForbidden, []byte("[]"))
			Expect(err).To(MatchError("the API returned HTTP 403: []"))
			Expect(err.(interface{ Hint() string }).Hint()).To(ContainSubstring("fft auth whoami"))
		})

		It("does not take a JSON null for the envelope", func() {
			err := client.Check(http.StatusForbidden, []byte("null"))
			Expect(err).To(MatchError(ContainSubstring("a proxy or gateway between fft and the tenant")))
		})

		It("does not blame a proxy for a bare 403", func() {
			err := client.Check(http.StatusForbidden, nil)
			Expect(err).To(MatchError("the API returned HTTP 403"))
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
		Expect(exitcode.FromError(err)).To(Equal(exitcode.Unavailable))
	})

	It("still reads as the interrupt it is when the cancellation sits inside a transport failure", func() {
		wrapped := &url.Error{Op: "Get", URL: "https://acme.api.fulfillmenttools.com/api/status", Err: context.Canceled}

		err := client.RequestError("reach the tenant", wrapped)

		Expect(exitcode.FromError(err)).To(Equal(exitcode.Interrupted))
	})

	It("lets an error that is not a transport failure keep the code it has", func() {
		err := client.RequestError("reach the tenant", errors.New("marshal the body"))

		Expect(exitcode.FromError(err)).To(Equal(exitcode.General))
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

	// #72: the bearer token is attached by the transport on every hop, not on the
	// caller's req.Header, so Go's stdlib cross-domain header stripping never
	// engages. Without a redirect guard the live token would follow a 3xx from the
	// base host to an attacker host — confirmed with a PoC. This asserts it does not.
	It("refuses a cross-host redirect, so the token never reaches another host", func() {
		var attackerAuth string
		attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attackerAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		DeferCleanup(attacker.Close)

		tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, attacker.URL, http.StatusFound)
		}))
		DeferCleanup(tenant.Close)

		c, err := client.New(tenant.URL, client.WithTokenSource(auth.StaticTokenSource("SECRET")))
		Expect(err).NotTo(HaveOccurred())

		_, err = c.API().StatusWithResponse(context.Background())

		Expect(err).To(MatchError(ContainSubstring("refusing cross-origin redirect")))
		Expect(attackerAuth).To(BeEmpty(), "the token must never leave for another host")
	})

	// A same-host https→http downgrade keeps the Host but drops TLS, so the guard
	// must catch it on Scheme too, or the live token rides the downgraded hop in
	// cleartext (CWE-319). The redirect target is the same host:port as the TLS
	// server, so only the scheme differs — pinRedirect refuses it before the
	// plaintext request is ever made.
	It("refuses a same-host scheme downgrade, so the token is never sent in cleartext", func() {
		tenant := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://"+r.Host+"/elsewhere", http.StatusFound)
		}))
		DeferCleanup(tenant.Close)

		c, err := client.New(tenant.URL,
			client.WithTokenSource(auth.StaticTokenSource("SECRET")),
			client.WithHTTPClient(tenant.Client()))
		Expect(err).NotTo(HaveOccurred())

		_, err = c.API().StatusWithResponse(context.Background())

		Expect(err).To(MatchError(ContainSubstring("refusing cross-origin redirect")))
	})

	// The 10-hop cap must error like Go's default policy, not hand back the in-flight
	// 3xx as a success — a same-host redirect loop should fail loudly, not surface as
	// a baffling "the API returned HTTP 302".
	It("errors after the redirect limit rather than returning the 3xx as success", func() {
		tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/loop", http.StatusFound)
		}))
		DeferCleanup(tenant.Close)

		c, err := client.New(tenant.URL, client.WithTokenSource(auth.StaticTokenSource("tok")))
		Expect(err).NotTo(HaveOccurred())

		_, err = c.API().StatusWithResponse(context.Background())

		Expect(err).To(MatchError(ContainSubstring("stopped after 10 redirects")))
	})

	It("still follows a same-host redirect, carrying the token to the final hop", func() {
		var finalAuth string
		mux := http.NewServeMux()
		mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		})
		mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, r *http.Request) {
			finalAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"status":"OK"}`))
			Expect(err).NotTo(HaveOccurred())
		})
		same := httptest.NewServer(mux)
		DeferCleanup(same.Close)

		c, err := client.New(same.URL, client.WithTokenSource(auth.StaticTokenSource("tok")))
		Expect(err).NotTo(HaveOccurred())

		res, err := c.API().StatusWithResponse(context.Background())

		Expect(err).NotTo(HaveOccurred())
		Expect(res.StatusCode()).To(Equal(http.StatusOK))
		Expect(finalAuth).To(Equal("Bearer tok"))
	})

	// The origin comparison must normalize the way net/url does not: host case and a
	// scheme-default port are not a different origin. Refusing those would fail closed
	// (no leak) but break a legitimate gateway/CDN redirect. httptest can't reproduce
	// them (it always uses 127.0.0.1 with an explicit non-default port), so pinRedirect
	// is exercised directly.
	DescribeTable("pinning the origin without spurious refusals",
		func(from, to string, wantRefused bool) {
			mkReq := func(raw string) *http.Request {
				u, err := url.Parse(raw)
				Expect(err).NotTo(HaveOccurred())
				return &http.Request{URL: u}
			}

			err := client.PinRedirect(mkReq(to), []*http.Request{mkReq(from)})

			if wantRefused {
				Expect(err).To(MatchError(ContainSubstring("refusing cross-origin redirect")))
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
		},
		Entry("host case differs but the origin is the same",
			"https://ACME.example.com/x", "https://acme.example.com/y", false),
		Entry("explicit :443 equals the implicit https port",
			"https://t.example.com/x", "https://t.example.com:443/y", false),
		Entry("explicit :80 equals the implicit http port",
			"http://t.example.com:80/x", "http://t.example.com/y", false),
		Entry("a different host is refused",
			"https://t.example.com/x", "https://evil.example.com/y", true),
		Entry("a scheme downgrade is refused",
			"https://t.example.com/x", "http://t.example.com/y", true),
		Entry("a different non-default port is refused",
			"https://t.example.com:8443/x", "https://t.example.com:9443/y", true),
	)
})

var _ = Describe("a proxy that refuses to open the tunnel to the tenant", func() {
	// net/http turns any non-200 CONNECT reply into a bare transport error, so this
	// never arrives as a response for Check to classify. These specs go through a real
	// proxy and the real transport for that reason.
	DescribeTable("is named as the proxy's refusal, with a hint that is not about permissions",
		func(status int) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodConnect))
				w.WriteHeader(status)
			}))
			DeferCleanup(proxy.Close)
			proxyURL, err := url.Parse(proxy.URL)
			Expect(err).NotTo(HaveOccurred())
			proxyURL.User = url.UserPassword("svc", "s3cret")

			hc := &http.Client{Transport: client.TunnelTransport(proxyURL)}
			_, err = hc.Get("https://tenant.example.test/api/facilities") //nolint:noctx // a spec against a stub proxy
			Expect(err).To(HaveOccurred())

			err = client.RequestError("list facilities", err)

			var refused *client.ProxyRefusedError
			Expect(errors.As(err, &refused)).To(BeTrue())
			Expect(refused.Status).To(Equal(status))
			Expect(err.Error()).To(ContainSubstring(http.StatusText(status)))
			// The proxy's credentials ride in its URL; the message must carry the host only.
			Expect(err.Error()).NotTo(ContainSubstring("s3cret"))
			Expect(err.(interface{ Hint() string }).Hint()).To(ContainSubstring("HTTPS_PROXY"))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.General))
		},
		Entry("403", http.StatusForbidden),
		Entry("407", http.StatusProxyAuthRequired),
	)

	// A 5xx from the proxy is the proxy being down, which passes; a 403 or 407 is its
	// policy or its credentials, which retrying does not change. Only the first is 9.
	DescribeTable("exits with the code that says whether to try again",
		func(status int, want int) {
			Expect(exitcode.FromError(&client.ProxyRefusedError{Status: status, Proxy: "proxy.example.test:3128"})).To(Equal(want))
		},
		Entry("502 Bad Gateway", http.StatusBadGateway, exitcode.Unavailable),
		Entry("503 Service Unavailable", http.StatusServiceUnavailable, exitcode.Unavailable),
		Entry("504 Gateway Timeout", http.StatusGatewayTimeout, exitcode.Unavailable),
		Entry("403 Forbidden", http.StatusForbidden, exitcode.General),
		Entry("407 Proxy Authentication Required", http.StatusProxyAuthRequired, exitcode.General),
	)
})
