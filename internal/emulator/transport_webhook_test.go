package emulator

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("webhookTransport", func() {
	webhookTarget := func(url string) map[string]any {
		return map[string]any{"type": targetWebhook, "callbackUrl": url}
	}

	Describe("plan", func() {
		local := newWebhookTransport(false)

		It("labels a delivery with the callback URL", func() {
			d, err := local.plan(webhookTarget("http://localhost:3000/hook"))
			Expect(err).NotTo(HaveOccurred())
			Expect(d.label).To(Equal("http://localhost:3000/hook"))
		})

		It("refuses a target with no callbackUrl", func() {
			_, err := local.plan(map[string]any{"type": targetWebhook})
			Expect(err).To(MatchError(ContainSubstring("no callbackUrl")))
		})

		It("refuses a scheme that is not http or https", func() {
			_, err := local.plan(webhookTarget("ftp://localhost/hook"))
			Expect(err).To(MatchError(ContainSubstring("not http or https")))
		})

		It("refuses a URL with no host", func() {
			_, err := local.plan(webhookTarget("http:///hook"))
			Expect(err).To(MatchError(ContainSubstring("no host")))
		})

		DescribeTable("accepts a host that can only be local",
			func(url string) {
				_, err := local.plan(webhookTarget(url))
				Expect(err).NotTo(HaveOccurred())
			},
			Entry("loopback address", "http://127.0.0.1:3000/hook"),
			Entry("IPv6 loopback", "http://[::1]:3000/hook"),
			Entry("localhost", "http://localhost/hook"),
			Entry("a private address", "http://10.1.2.3/hook"),
			Entry("a single-label name, as on a Docker network", "http://app:3000/hook"),
			Entry("the Docker host alias", "http://host.docker.internal:3000/hook"),
			Entry("a reserved local suffix", "http://api.internal/hook"),
		)

		It("refuses a remote host, naming the flag that would allow it", func() {
			_, err := local.plan(webhookTarget("https://example.com/hook"))
			Expect(err).To(MatchError(ContainSubstring("--webhook-allow-remote")))
		})

		DescribeTable("refuses the cloud metadata endpoint even in local mode",
			// 169.254.169.254 is link-local, so the local allowlist would otherwise
			// wave it through — and a POST there with attacker-chosen headers is a
			// blind-SSRF read of instance credentials.
			func(url string) {
				_, err := local.plan(webhookTarget(url))
				Expect(err).To(HaveOccurred())
			},
			Entry("IMDS v4", "http://169.254.169.254/latest/meta-data/"),
			Entry("IMDS IPv6", "http://[fd00:ec2::254]/latest/meta-data/"),
		)

		It("accepts a remote host once widened", func() {
			_, err := newWebhookTransport(true).plan(webhookTarget("https://example.com/hook"))
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("send", func() {
		var (
			transport *webhookTransport
			requests  chan *http.Request
			bodies    chan []byte
			status    int
			endpoint  string
		)

		BeforeEach(func() {
			transport = newWebhookTransport(false)
			requests, bodies, status = make(chan *http.Request, 1), make(chan []byte, 1), http.StatusOK

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requests <- r
				bodies <- body
				w.WriteHeader(status)
			}))
			DeferCleanup(srv.Close)
			endpoint = srv.URL + "/hook"
		})

		It("POSTs the envelope as JSON with the target's headers", func() {
			d, err := transport.plan(map[string]any{
				"type":        targetWebhook,
				"callbackUrl": endpoint,
				"headers": []any{
					map[string]any{"key": "Authorization", "value": "Basic dXNlcg=="},
					map[string]any{"key": "X-Tenant", "value": "acme"},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(d.send(context.Background(), "ORDER_CREATED", []byte(`{"event":"ORDER_CREATED"}`))).To(Succeed())

			req := <-requests
			Expect(req.Method).To(Equal(http.MethodPost))
			Expect(req.Header.Get("Content-Type")).To(Equal("application/json"))
			Expect(req.Header.Get("Authorization")).To(Equal("Basic dXNlcg=="))
			Expect(req.Header.Get("X-Tenant")).To(Equal("acme"))
			Expect(<-bodies).To(MatchJSON(`{"event":"ORDER_CREATED"}`))
		})

		It("reports a non-2xx answer as a failed delivery", func() {
			status = http.StatusInternalServerError

			d, err := transport.plan(webhookTarget(endpoint))
			Expect(err).NotTo(HaveOccurred())
			Expect(d.send(context.Background(), "ORDER_CREATED", []byte(`{}`))).
				To(MatchError(ContainSubstring("500")))
		})

		// The local-host check is worth nothing if a local endpoint can answer 302 and
		// have the delivery walked to a remote host, so the refusal to follow redirects
		// is asserted rather than left to a comment.
		It("does not follow a redirect out of the local network", func() {
			var remote atomic.Int64
			elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				remote.Add(1)
			}))
			DeferCleanup(elsewhere.Close)

			bounce := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, elsewhere.URL+"/hook", http.StatusFound)
			}))
			DeferCleanup(bounce.Close)

			d, err := transport.plan(webhookTarget(bounce.URL + "/hook"))
			Expect(err).NotTo(HaveOccurred())
			Expect(d.send(context.Background(), "ORDER_CREATED", []byte(`{}`))).
				To(MatchError(ContainSubstring("302")))
			Expect(remote.Load()).To(BeZero())
		})
	})
})
