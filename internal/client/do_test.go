package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// idp stands in for Google. It counts how often fft went back for a new token,
// which is the number the reactive-401 spec is really about: one refresh, not one
// per attempt and not none.
type idp struct {
	tokens   atomic.Int64
	renewals atomic.Int64
}

func (i *idp) Token(context.Context) (string, error) {
	i.tokens.Add(1)
	return fmt.Sprintf("token-%d", i.renewals.Load()), nil
}

func (i *idp) Renew(context.Context) (auth.Token, error) {
	i.renewals.Add(1)
	return auth.Token{ID: fmt.Sprintf("token-%d", i.renewals.Load())}, nil
}

var _ auth.TokenSource = (*idp)(nil)
var _ auth.Renewer = (*idp)(nil)

// tenant is a fake fulfillmenttools that records every request it was sent.
//
// Each request is served on its own goroutine, so the recording is behind a mutex:
// the requests are sequential in time, but nothing tells the race detector that.
type tenant struct {
	*httptest.Server

	mu      sync.Mutex
	methods []string
	tokens  []string
	bodies  [][]byte
	queries []url.Values
	handler func(w http.ResponseWriter, r *http.Request, n int)
}

func newTenant(handler func(w http.ResponseWriter, r *http.Request, n int)) *tenant {
	t := &tenant{handler: handler}
	t.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		Expect(err).NotTo(HaveOccurred())

		t.mu.Lock()
		t.methods = append(t.methods, r.Method)
		t.tokens = append(t.tokens, r.Header.Get("Authorization"))
		t.bodies = append(t.bodies, body)
		t.queries = append(t.queries, r.URL.Query())
		n := len(t.methods)
		t.mu.Unlock()

		t.handler(w, r, n)
	}))
	DeferCleanup(t.Close)
	return t
}

// asked returns the query string of the nth request (counting from zero). A cursor
// search carries its paging in the body and a GET list carries it in the query, so
// the pagination specs of list.go assert on this where the search specs assert on
// [tenant.sent].
func (t *tenant) asked(n int) url.Values {
	GinkgoHelper()

	t.mu.Lock()
	defer t.mu.Unlock()

	Expect(n).To(BeNumerically("<", len(t.queries)), "the tenant was not sent that many requests")
	return t.queries[n]
}

func (t *tenant) hits() int { return len(t.seen()) }

func (t *tenant) seen() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.methods)
}

func (t *tenant) bearers() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.tokens)
}

// sent decodes the body of the nth request (counting from zero), which is how the
// pagination specs assert that the cursor was actually followed.
func (t *tenant) sent(n int) searched {
	GinkgoHelper()

	t.mu.Lock()
	defer t.mu.Unlock()

	Expect(t.bodies).To(HaveLen(len(t.methods)))
	Expect(n).To(BeNumerically("<", len(t.bodies)), "the tenant was not sent that many requests")

	var body searched
	Expect(json.Unmarshal(t.bodies[n], &body)).To(Succeed())
	return body
}

// noWait is the retry policy the specs use: the same decisions, none of the
// waiting. The durations it was asked to sleep for are recorded, so that "the
// Retry-After was honoured" is an assertion rather than a stopwatch.
type noWait struct{ slept []time.Duration }

func (n *noWait) retry() client.Retry {
	return client.Retry{
		Base: time.Millisecond,
		Max:  2 * time.Millisecond,
		Sleep: func(ctx context.Context, d time.Duration) error {
			n.slept = append(n.slept, d)
			return ctx.Err()
		},
	}
}

// get is the simplest possible request through the client: GET /api/status.
func get(c *client.Client) client.Doer {
	return func(ctx context.Context) (*http.Response, error) {
		return c.API().Status(ctx)
	}
}

// post is a create: POST /api/facilities. It is the request that must never be
// sent twice.
func post(c *client.Client) client.Doer {
	return func(ctx context.Context) (*http.Response, error) {
		return c.API().AddFacility(ctx, api.AddFacilityJSONRequestBody{})
	}
}

var _ = Describe("sending a request", func() {
	var (
		ctx    context.Context
		google *idp
		policy *noWait
	)

	BeforeEach(func() {
		ctx = context.Background()
		google = &idp{}
		policy = &noWait{}
	})

	newClient := func(t *tenant) *client.Client {
		c, err := client.New(t.URL,
			client.WithTokenSource(google),
			client.WithRetry(policy.retry()))
		Expect(err).NotTo(HaveOccurred())
		return c
	}

	Describe("a token the API no longer accepts", func() {
		// The 401 retry lives here and not in the RoundTripper: a RoundTripper is
		// handed a body it has already consumed by the time the 401 comes back.
		When("the first request is refused", func() {
			var t *tenant

			BeforeEach(func() {
				t = newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
					if n == 1 {
						w.WriteHeader(http.StatusUnauthorized)
						fmt.Fprint(w, `[{"summary":"the token has expired"}]`)
						return
					}
					fmt.Fprint(w, `{"status":"OK"}`)
				})
			})

			It("refreshes the token and retries, and the second request succeeds", func() {
				c := newClient(t)

				res, err := c.Do(ctx, "check the status", get(c))

				Expect(err).NotTo(HaveOccurred())
				Expect(res.Status).To(Equal(http.StatusOK))
			})

			It("goes back to the API exactly twice, and to the identity provider exactly once", func() {
				c := newClient(t)

				_, err := c.Do(ctx, "check the status", get(c))

				Expect(err).NotTo(HaveOccurred())
				Expect(t.hits()).To(Equal(2))
				Expect(google.renewals.Load()).To(BeEquivalentTo(1))
			})

			It("retries with the new token, not the one that was just refused", func() {
				c := newClient(t)

				_, err := c.Do(ctx, "check the status", get(c))

				Expect(err).NotTo(HaveOccurred())
				Expect(t.bearers()).To(Equal([]string{"Bearer token-0", "Bearer token-1"}))
			})
		})

		When("the retried request is refused as well", func() {
			// The bool that guards the refresh is what makes this a spec about
			// termination rather than about timing: a second 401 cannot reach the
			// refresh branch at all, so there is no loop to get lucky with.
			It("gives up after one retry rather than refreshing forever", func() {
				t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprint(w, `[{"summary":"the token has expired"}]`)
				})
				c := newClient(t)

				_, err := c.Do(ctx, "check the status", get(c))

				Expect(err).To(MatchError(ContainSubstring("the token has expired")))
				Expect(t.hits()).To(Equal(2))
				Expect(google.renewals.Load()).To(BeEquivalentTo(1))
			})

			It("exits 4, so a script can tell an authentication failure from any other", func() {
				t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprint(w, `[{"summary":"nope"}]`)
				})
				c := newClient(t)

				_, err := c.Do(ctx, "check the status", get(c))

				Expect(exitcode.FromError(err)).To(Equal(exitcode.Auth))
			})
		})

		When("the token source has nothing to refresh from", func() {
			It("surfaces the 401 rather than sending the same dead token twice", func() {
				// FFT_ID_TOKEN: a fixed string with no password and no refresh token
				// behind it. Retrying it would be sending the identical request.
				t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprint(w, `[{"summary":"nope"}]`)
				})
				c, err := client.New(t.URL,
					client.WithTokenSource(auth.StaticTokenSource("stale")),
					client.WithRetry(policy.retry()))
				Expect(err).NotTo(HaveOccurred())

				_, err = c.Do(ctx, "check the status", get(c))

				Expect(exitcode.FromError(err)).To(Equal(exitcode.Auth))
				Expect(t.hits()).To(Equal(1))
			})
		})
	})

	Describe("a request the API asks us to slow down", func() {
		// A 429 means the request provably did not happen, so it is safe to repeat
		// whatever the method — including a create.
		It("waits as long as Retry-After says, and retries a GET", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
				if n == 1 {
					w.Header().Set("Retry-After", "2")
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				fmt.Fprint(w, `{"status":"OK"}`)
			})
			c := newClient(t)

			_, err := c.Do(ctx, "check the status", get(c))

			Expect(err).NotTo(HaveOccurred())
			Expect(t.hits()).To(Equal(2))
			Expect(policy.slept).To(Equal([]time.Duration{2 * time.Second}))
		})

		It("retries a POST too, because a 429 is a request that never ran", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
				if n == 1 {
					w.Header().Set("Retry-After", "1")
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, `{"name":"warehouse"}`)
			})
			c := newClient(t)

			res, err := c.Do(ctx, "create the facility", post(c))

			Expect(err).NotTo(HaveOccurred())
			Expect(res.Status).To(Equal(http.StatusCreated))
			Expect(t.seen()).To(Equal([]string{http.MethodPost, http.MethodPost}))
			Expect(policy.slept).To(Equal([]time.Duration{time.Second}))
		})

		It("gives up after the third attempt rather than waiting forever", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
			})
			c := newClient(t)

			_, err := c.Do(ctx, "check the status", get(c))

			Expect(err).To(HaveOccurred())
			Expect(t.hits()).To(Equal(3))
		})

		It("does not sit out a rate limit measured in minutes; it says so instead", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				w.Header().Set("Retry-After", "600")
				w.WriteHeader(http.StatusTooManyRequests)
			})
			c := newClient(t)

			_, err := c.Do(ctx, "check the status", get(c))

			Expect(err).To(MatchError(ContainSubstring("429")))
			Expect(t.hits()).To(Equal(1))
			Expect(policy.slept).To(BeEmpty())
		})
	})

	Describe("a request the API failed to answer", func() {
		// This is the rule that matters most in the whole package. POST /api/facilities
		// and POST /api/stocks are creates: a 500 does not say the facility was not
		// created, it says we were not told whether it was.
		It("never retries a POST on a 500 — the resource may already exist", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `[{"summary":"internal error"}]`)
			})
			c := newClient(t)

			_, err := c.Do(ctx, "create the facility", post(c))

			Expect(err).To(MatchError(ContainSubstring("internal error")))
			Expect(t.hits()).To(Equal(1))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Unavailable))
		})

		DescribeTable("retries the methods that may safely be repeated",
			func(method string, want int) {
				t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, `[{"summary":"internal error"}]`)
				})
				c := newClient(t)

				do := func(ctx context.Context) (*http.Response, error) {
					req, err := http.NewRequestWithContext(ctx, method, t.URL+"/api/facilities/x", nil)
					if err != nil {
						return nil, err
					}
					return http.DefaultClient.Do(req)
				}

				_, err := c.Do(ctx, "act on the facility", do)

				Expect(err).To(HaveOccurred())
				Expect(t.hits()).To(Equal(want))
			},
			Entry("a GET reads, so repeating it changes nothing", http.MethodGet, 3),
			Entry("a PUT is idempotent by definition", http.MethodPut, 3),
			Entry("a DELETE of the same id twice is still one deletion", http.MethodDelete, 3),
			Entry("a PATCH is not idempotent in this API", http.MethodPatch, 1),
			Entry("a POST creates, and may have created", http.MethodPost, 1),
		)

		It("succeeds on the retry when the API recovers", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
				if n == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				fmt.Fprint(w, `{"status":"OK"}`)
			})
			c := newClient(t)

			res, err := c.Do(ctx, "check the status", get(c))

			Expect(err).NotTo(HaveOccurred())
			Expect(res.Status).To(Equal(http.StatusOK))
			Expect(t.hits()).To(Equal(2))
		})
	})

	Describe("a request that was cancelled", func() {
		It("stops rather than backing off into a timeout that has already passed", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				w.WriteHeader(http.StatusInternalServerError)
			})
			c := newClient(t)

			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			_, err := c.Do(cancelled, "check the status", get(c))

			Expect(err).To(MatchError(context.Canceled))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Interrupted))
		})
	})

	Describe("an answer that is not the documented envelope", func() {
		It("reports a 502 of HTML as a 502, not as an unmarshal error", func() {
			// A CDN or a proxy in front of the tenant answers with an HTML error page.
			// "invalid character '<' looking for beginning of value" tells the user
			// nothing they can act on.
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusBadGateway)
				fmt.Fprint(w, "<html><body>502 Bad Gateway</body></html>")
			})
			c := newClient(t)

			_, err := c.Do(ctx, "check the status", get(c))

			var apiErr *client.APIError
			Expect(errors.As(err, &apiErr)).To(BeTrue())
			Expect(apiErr.Status).To(Equal(http.StatusBadGateway))
			Expect(err).To(MatchError(ContainSubstring("502 Bad Gateway")))
			Expect(err).NotTo(MatchError(ContainSubstring("invalid character")))
		})
	})
})
