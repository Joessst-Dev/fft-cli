package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// failingSource stands in for a token source that cannot mint — the network is
// down, or the user must sign in again.
type failingSource struct{ err error }

func (s failingSource) Token(context.Context) (string, error) { return "", s.err }

var _ = Describe("the fulfillmenttools transport", func() {
	var (
		tenant  *httptest.Server
		gotReqs []*http.Request
		hc      *http.Client
	)

	BeforeEach(func() {
		gotReqs = nil

		tenant = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotReqs = append(gotReqs, r.Clone(r.Context()))
			w.WriteHeader(http.StatusOK)
		}))
		DeferCleanup(tenant.Close)

		hc = &http.Client{Transport: &Transport{Source: StaticTokenSource("the-id-token")}}
	})

	It("presents the id token as a bearer credential", func() {
		res, err := hc.Get(tenant.URL + "/api/facilities")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Body.Close()).To(Succeed())

		Expect(gotReqs).To(HaveLen(1))
		Expect(gotReqs[0].Header.Get("Authorization")).To(Equal("Bearer the-id-token"))
	})

	// This is the invariant the whole package is arranged around. The Firebase Web
	// API key is Google's, it identifies a Firebase project, and a fulfillmenttools
	// tenant has no business receiving it. Assert it at the wire, not at the call
	// site: the wire is what actually leaves the machine.
	It("sends no API key to the tenant — not as a parameter, not as a header", func() {
		res, err := hc.Get(tenant.URL + "/api/facilities?status=ACTIVE")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Body.Close()).To(Succeed())

		req := gotReqs[0]

		Expect(req.URL.Query()).NotTo(HaveKey("key"))
		Expect(req.URL.RawQuery).NotTo(ContainSubstring("key="))
		for _, header := range []string{"X-Goog-Api-Key", "X-Api-Key", "Api-Key"} {
			Expect(req.Header.Get(header)).To(BeEmpty())
		}
		for name, values := range req.Header {
			Expect(strings.Join(values, " ")).NotTo(ContainSubstring(testAPIKey),
				"header %s carries the Firebase API key", name)
		}
	})

	It("leaves the caller's request untouched, as an http.RoundTripper must", func() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, tenant.URL, nil)
		Expect(err).NotTo(HaveOccurred())

		res, err := hc.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Body.Close()).To(Succeed())

		Expect(req.Header).NotTo(HaveKey("Authorization"))
	})

	When("no token can be minted", func() {
		It("never sends the request, so the tenant sees no anonymous call", func() {
			hc = &http.Client{Transport: &Transport{Source: failingSource{err: ErrReauthRequired}}}

			_, err := hc.Get(tenant.URL + "/api/facilities")

			Expect(err).To(MatchError(ErrReauthRequired))
			Expect(gotReqs).To(BeEmpty())
		})
	})

	When("it has no token source at all", func() {
		It("fails rather than sending an unauthenticated request", func() {
			hc = &http.Client{Transport: &Transport{}}

			_, err := hc.Get(tenant.URL + "/api/facilities")

			Expect(err).To(MatchError(ContainSubstring("no token source")))
			Expect(gotReqs).To(BeEmpty())
		})
	})
})

var _ = Describe("an authenticated request driven by a real Firebase source", func() {
	// End to end within this package: a token minted from the fake Google servers,
	// carried to a fake tenant. The key goes to Google; the tenant gets a bearer
	// token and nothing more.
	It("takes the key to Google and only the bearer token to the tenant", func() {
		g, clk := newGoogle(), newClock()
		src := NewFirebaseTokenSource(g.client(clk.Now), testProjectConfig(), storeWithPassword(testPassword), clk.Now)

		var tenantReq *http.Request
		tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantReq = r.Clone(r.Context())
			w.WriteHeader(http.StatusOK)
		}))
		DeferCleanup(tenant.Close)

		hc := &http.Client{Transport: &Transport{Source: src}}
		res, err := hc.Get(tenant.URL + "/api/status")
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Body.Close()).To(Succeed())

		Expect(g.receivedKeys()).To(ConsistOf(testAPIKey))
		Expect(tenantReq.Header.Get("Authorization")).To(Equal("Bearer id-from-signin-1"))
		Expect(tenantReq.URL.RawQuery).To(BeEmpty())
	})

	It("refuses the request when authentication has run out of options", func() {
		g, clk := newGoogle(), newClock()
		g.failSignIn(http.StatusBadRequest, "INVALID_LOGIN_CREDENTIALS")
		src := NewFirebaseTokenSource(g.client(clk.Now), testProjectConfig(), storeWithPassword("stale"), clk.Now)

		hc := &http.Client{Transport: &Transport{Source: src}}
		_, err := hc.Get("https://acme.api.fulfillmenttools.com/api/status")

		Expect(errors.Is(err, ErrReauthRequired)).To(BeTrue())
	})
})
