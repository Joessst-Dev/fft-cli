package client_test

import (
	"context"
	"net/http"
	"net/url"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// raw is a fake tenant that records the *URL* of every request, which is the only
// place the explode encoding is visible at all.
type raw struct {
	mu      sync.Mutex
	urls    []*url.URL
	methods []string

	answer func(w http.ResponseWriter, r *http.Request, n int)
}

func (t *raw) record(w http.ResponseWriter, r *http.Request, n int) {
	t.mu.Lock()
	t.urls = append(t.urls, r.URL)
	t.methods = append(t.methods, r.Method)
	t.mu.Unlock()

	t.answer(w, r, n)
}

func (t *raw) rawQuery(n int) string {
	GinkgoHelper()

	t.mu.Lock()
	defer t.mu.Unlock()

	Expect(n).To(BeNumerically("<", len(t.urls)), "the tenant was not sent that many requests")
	return t.urls[n].RawQuery
}

var _ = Describe("Client.DoRaw", func() {
	var (
		fake *raw
		c    *client.Client
	)

	// ok is a tenant that answers every request with an empty JSON object.
	ok := func() {
		fake = &raw{answer: func(w http.ResponseWriter, _ *http.Request, _ int) {
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{}`))
			Expect(err).NotTo(HaveOccurred())
		}}

		srv := newTenant(fake.record)

		var err error
		c, err = client.New(srv.URL, client.WithTokenSource(auth.StaticTokenSource("t")))
		Expect(err).NotTo(HaveOccurred())
	}

	// The bug class this whole tier exists to avoid. The API answers 200 to either
	// encoding, so the wrong one is not an error — it is a filter that quietly matches
	// the wrong rows, and the user is told there are no pickjobs.
	Describe("the explode encoding", func() {
		BeforeEach(ok)

		It("comma-joins the values of an explode:false parameter", func() {
			_, err := c.DoRaw(context.Background(), "list the pickjobs", client.RawRequest{
				Method: http.MethodGet,
				Path:   "/api/pickjobs",
				Query: []client.QueryParam{
					{Name: "status", Values: []string{"OPEN", "IN_PROGRESS"}, Explode: false},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// One parameter, two values, joined with a comma. The comma is percent-encoded,
			// which is what url.Values.Encode does and what the generated client already
			// sends for the five tags it covers.
			Expect(fake.rawQuery(0)).To(Equal("status=OPEN%2CIN_PROGRESS"))
		})

		It("repeats the name of an explode:true parameter", func() {
			_, err := c.DoRaw(context.Background(), "list the facilities", client.RawRequest{
				Method: http.MethodGet,
				Path:   "/api/facilities",
				Query: []client.QueryParam{
					{Name: "status", Values: []string{"ONLINE", "OFFLINE"}, Explode: true},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.rawQuery(0)).To(Equal("status=ONLINE&status=OFFLINE"))
		})

		It("encodes each parameter by its own rule, in the same request", func() {
			// This is the case a single global policy cannot get right, and the reason the
			// flag is carried per parameter rather than decided once.
			_, err := c.DoRaw(context.Background(), "search", client.RawRequest{
				Method: http.MethodGet,
				Path:   "/api/things",
				Query: []client.QueryParam{
					{Name: "joined", Values: []string{"a", "b"}, Explode: false},
					{Name: "repeated", Values: []string{"a", "b"}, Explode: true},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.rawQuery(0)).To(Equal("joined=a%2Cb&repeated=a&repeated=b"))
		})

		It("sends a single value the same way under either encoding", func() {
			for _, explode := range []bool{true, false} {
				_, err := c.DoRaw(context.Background(), "search", client.RawRequest{
					Method: http.MethodGet,
					Path:   "/api/things",
					Query:  []client.QueryParam{{Name: "status", Values: []string{"OPEN"}, Explode: explode}},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(fake.rawQuery(0)).To(Equal("status=OPEN"))
			Expect(fake.rawQuery(1)).To(Equal("status=OPEN"))
		})

		It("sends nothing at all for a parameter with no values", func() {
			// `?status=` is not "no filter", it is a filter on the empty string.
			_, err := c.DoRaw(context.Background(), "search", client.RawRequest{
				Method: http.MethodGet,
				Path:   "/api/things",
				Query:  []client.QueryParam{{Name: "status", Values: nil, Explode: true}},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.rawQuery(0)).To(BeEmpty())
		})
	})

	Describe("the path template", func() {
		BeforeEach(ok)

		It("fills the placeholders", func() {
			_, err := c.DoRaw(context.Background(), "get the pickjob", client.RawRequest{
				Method:     http.MethodGet,
				Path:       "/api/pickjobs/{pickJobId}",
				PathParams: map[string]string{"pickJobId": "abc123"},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.urls[0].Path).To(Equal("/api/pickjobs/abc123"))
		})

		It("leaves the colons in a facility URN alone", func() {
			// Every facility path parameter also accepts
			// urn:fft:facility:tenantFacilityId:<id>. Escaping the colons would turn that
			// into an id that matches nothing — and the API would answer 404, or worse, 200.
			_, err := c.DoRaw(context.Background(), "get the facility", client.RawRequest{
				Method:     http.MethodGet,
				Path:       "/api/facilities/{facilityId}",
				PathParams: map[string]string{"facilityId": client.FacilityRef("0090000020")},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.urls[0].Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:0090000020"))
		})

		It("escapes a slash in a value rather than inventing a path segment", func() {
			_, err := c.DoRaw(context.Background(), "get the thing", client.RawRequest{
				Method:     http.MethodGet,
				Path:       "/api/things/{id}",
				PathParams: map[string]string{"id": "a/b"},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.urls[0].EscapedPath()).To(Equal("/api/things/a%2Fb"))
			Expect(fake.urls[0].Path).To(Equal("/api/things/a/b"))
		})

		DescribeTable("refuses a request it cannot address, and sends nothing",
			func(req client.RawRequest, complaint string) {
				_, err := c.DoRaw(context.Background(), "get the thing", req)

				Expect(err).To(MatchError(ContainSubstring(complaint)))
				Expect(fake.urls).To(BeEmpty(), "a request was sent although the URL was unbuildable")
			},

			Entry("a placeholder with no value",
				client.RawRequest{Method: http.MethodGet, Path: "/api/things/{id}"},
				"has no value for id"),

			// An empty value would collapse /api/things/{id} to /api/things/ — a different
			// endpoint, which answers 200 with a list.
			Entry("a placeholder with an empty value",
				client.RawRequest{
					Method: http.MethodGet, Path: "/api/things/{id}",
					PathParams: map[string]string{"id": ""},
				},
				"has no value for id"),

			Entry("a value for a placeholder that does not exist",
				client.RawRequest{
					Method: http.MethodGet, Path: "/api/things",
					PathParams: map[string]string{"id": "x"},
				},
				"has no placeholder for id"),
		)
	})

	Describe("going through Client.Do", func() {
		It("signs the request", func() {
			ok()

			_, err := c.DoRaw(context.Background(), "get", client.RawRequest{
				Method: http.MethodGet, Path: "/api/things",
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.urls).To(HaveLen(1))
		})

		It("decodes the error envelope as the JSON array it actually is", func() {
			fake = &raw{answer: func(w http.ResponseWriter, _ *http.Request, _ int) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, err := w.Write([]byte(`[{"summary":"no pickjob matching request was found"}]`))
				Expect(err).NotTo(HaveOccurred())
			}}
			srv := newTenant(fake.record)

			var err error
			c, err = client.New(srv.URL, client.WithTokenSource(auth.StaticTokenSource("t")))
			Expect(err).NotTo(HaveOccurred())

			_, err = c.DoRaw(context.Background(), "get the pickjob", client.RawRequest{
				Method: http.MethodGet, Path: "/api/pickjobs/{id}",
				PathParams: map[string]string{"id": "nope"},
			})

			Expect(err).To(MatchError(ContainSubstring("no pickjob matching request was found")))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.NotFound))
		})

		It("refreshes the token on a 401 and retries exactly once", func() {
			source := &idp{}

			fake = &raw{answer: func(w http.ResponseWriter, _ *http.Request, n int) {
				if n == 1 {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{}`))
				Expect(err).NotTo(HaveOccurred())
			}}
			srv := newTenant(fake.record)

			var err error
			c, err = client.New(srv.URL, client.WithTokenSource(source))
			Expect(err).NotTo(HaveOccurred())

			_, err = c.DoRaw(context.Background(), "get", client.RawRequest{
				Method: http.MethodGet, Path: "/api/things",
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.methods).To(HaveLen(2))
			Expect(source.renewals.Load()).To(BeNumerically("==", 1))
		})

		It("does not retry a POST that answered 500 — the resource may already exist", func() {
			var attempts int

			fake = &raw{answer: func(w http.ResponseWriter, _ *http.Request, n int) {
				attempts = n
				w.WriteHeader(http.StatusInternalServerError)
			}}
			srv := newTenant(fake.record)

			policy := &noWait{}

			var err error
			c, err = client.New(srv.URL,
				client.WithTokenSource(auth.StaticTokenSource("t")),
				client.WithRetry(policy.retry()))
			Expect(err).NotTo(HaveOccurred())

			_, err = c.DoRaw(context.Background(), "create the pickjob", client.RawRequest{
				Method: http.MethodPost,
				Path:   "/api/pickjobs",
				Body:   []byte(`{"tenantOrderId":"R1"}`),
			})

			Expect(err).To(HaveOccurred())
			Expect(attempts).To(Equal(1), "a POST that answered 500 was re-sent")
		})

		It("retries a GET that answered 500, with a fresh body reader each time", func() {
			fake = &raw{answer: func(w http.ResponseWriter, _ *http.Request, n int) {
				if n == 1 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{"ok":true}`))
				Expect(err).NotTo(HaveOccurred())
			}}
			srv := newTenant(fake.record)

			policy := &noWait{}

			var err error
			c, err = client.New(srv.URL,
				client.WithTokenSource(auth.StaticTokenSource("t")),
				client.WithRetry(policy.retry()))
			Expect(err).NotTo(HaveOccurred())

			res, err := c.DoRaw(context.Background(), "get", client.RawRequest{
				Method: http.MethodGet, Path: "/api/things",
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.methods).To(HaveLen(2))
			Expect(string(res.Body)).To(Equal(`{"ok":true}`))
		})

		It("sets Content-Type only when there is a body to describe", func() {
			var types []string

			fake = &raw{answer: func(w http.ResponseWriter, r *http.Request, _ int) {
				types = append(types, r.Header.Get("Content-Type"))
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{}`))
				Expect(err).NotTo(HaveOccurred())
			}}
			srv := newTenant(fake.record)

			var err error
			c, err = client.New(srv.URL, client.WithTokenSource(auth.StaticTokenSource("t")))
			Expect(err).NotTo(HaveOccurred())

			_, err = c.DoRaw(context.Background(), "get", client.RawRequest{
				Method: http.MethodGet, Path: "/api/things",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = c.DoRaw(context.Background(), "put", client.RawRequest{
				Method: http.MethodPut, Path: "/api/things", Body: []byte(`{}`),
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(types).To(Equal([]string{"", "application/json"}))
		})
	})
})
