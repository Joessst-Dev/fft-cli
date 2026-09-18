package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/client"
)

var _ = Describe("observing response statuses", func() {
	var (
		mu       sync.Mutex
		statuses []int
	)

	observe := func(status int) {
		mu.Lock()
		defer mu.Unlock()
		statuses = append(statuses, status)
	}

	seen := func() []int {
		mu.Lock()
		defer mu.Unlock()
		return append([]int(nil), statuses...)
	}

	BeforeEach(func() {
		mu.Lock()
		statuses = nil
		mu.Unlock()
	})

	get := func(c *client.Client) error {
		_, err := c.DoRaw(context.Background(), "get the pickjob", client.RawRequest{
			Method: http.MethodGet,
			Path:   "/api/pickjobs/pj-1",
		})
		return err
	}

	It("reports the status the tenant answered with, error or not", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`[{"summary":"no such pickjob"}]`))
		}))
		DeferCleanup(srv.Close)

		c, err := client.New(srv.URL,
			client.WithTokenSource(auth.StaticTokenSource("t")),
			client.WithObserver(observe))
		Expect(err).NotTo(HaveOccurred())

		Expect(get(c)).To(MatchError(ContainSubstring("no such pickjob")))
		Expect(seen()).To(Equal([]int{http.StatusNotFound}))
	})

	It("reports every attempt, so the last report is what the call finally got", func() {
		renewer := &idp{}
		t := newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`[{"summary":"expired"}]`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"pj-1"}`))
		})

		c, err := client.New(t.URL, client.WithTokenSource(renewer), client.WithObserver(observe))
		Expect(err).NotTo(HaveOccurred())

		Expect(get(c)).To(Succeed())
		Expect(seen()).To(Equal([]int{http.StatusUnauthorized, http.StatusOK}))
	})

	It("reports nothing for a request that never got an answer", func() {
		srv := httptest.NewServer(http.NotFoundHandler())
		addr := srv.URL
		srv.Close()

		c, err := client.New(addr,
			client.WithRetry(client.Retry{Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }}),
			client.WithObserver(observe))
		Expect(err).NotTo(HaveOccurred())

		Expect(get(c)).NotTo(Succeed())
		Expect(seen()).To(BeEmpty())
	})
})
