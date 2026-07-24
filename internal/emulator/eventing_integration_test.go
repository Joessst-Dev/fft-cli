package emulator

// These specs drive event publishing over a real socket, with a recording publisher
// in place of a real Pub/Sub emulator. They prove the wiring the unit tests cannot:
// that a CRUD mutation on the HTTP surface reaches the emitter, that the subscription
// store the create handler wrote is the one the emitter reads, and that the manual
// /_emulator/emit route publishes.

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("emulator eventing over HTTP", func() {
	var (
		baseURL string
		rec     *recordingTransport
	)

	BeforeEach(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())

		rec = recordingPubSub()
		srv, err := New(Config{transports: map[string]transport{targetGoogleCloudPubSub: rec}})
		Expect(err).NotTo(HaveOccurred())

		go func() { _ = srv.app.Listener(ln) }()
		DeferCleanup(func() { _ = srv.app.Shutdown() })

		baseURL = "http://" + ln.Addr().String()
		waitReady(baseURL)
	})

	It("publishes ORDER_CREATED when an order is created and a subscription matches", func() {
		registerPubSubSub(baseURL, "ORDER_CREATED", "local", "orders", nil)
		createOrder(baseURL, map[string]any{"tenantOrderId": "t"})

		msgs := rec.messages()
		Expect(msgs).To(HaveLen(1))
		Expect(msgs[0].label).To(Equal("local/orders"))

		var ev webHookEvent
		Expect(json.Unmarshal(msgs[0].data, &ev)).To(Succeed())
		Expect(ev.Event).To(Equal("ORDER_CREATED"))
		Expect(ev.EventID).NotTo(BeEmpty())
	})

	It("publishes nothing when no subscription is registered", func() {
		createOrder(baseURL, map[string]any{"tenantOrderId": "t"})
		Expect(rec.count()).To(Equal(0))
	})

	It("skips an event whose contexts the entity does not satisfy", func() {
		registerPubSubSub(baseURL, "ORDER_CREATED", "local", "orders", []any{
			map[string]any{"type": "FACILITY", "values": []any{"BER-01"}},
		})
		createOrder(baseURL, map[string]any{"tenantOrderId": "t"}) // references no facility

		Expect(rec.count()).To(Equal(0))
	})

	It("publishes a manually emitted event through /_emulator/emit", func() {
		registerPubSubSub(baseURL, "PICK_JOB_PICKING_COMMENCED", "local", "pick", nil)

		status, body := postJSON(baseURL, "/_emulator/emit", map[string]any{
			"event":   "PICK_JOB_PICKING_COMMENCED",
			"payload": map[string]any{"id": "pj1"},
		})
		Expect(status).To(Equal(http.StatusOK))

		var result struct {
			Published int      `json:"published"`
			Topics    []string `json:"topics"`
		}
		Expect(json.Unmarshal(body, &result)).To(Succeed())
		Expect(result.Published).To(Equal(1))
		Expect(result.Topics).To(ConsistOf("local/pick"))

		Expect(rec.messages()).To(HaveLen(1))
		Expect(rec.messages()[0].label).To(Equal("local/pick"))
	})

	It("rejects an emit with no event name", func() {
		status, _ := postJSON(baseURL, "/_emulator/emit", map[string]any{"payload": map[string]any{}})
		Expect(status).To(Equal(http.StatusBadRequest))
	})

	It("rejects an emit whose payload is not a JSON object", func() {
		status, _ := postJSON(baseURL, "/_emulator/emit", map[string]any{
			"event":   "PICK_JOB_PICKING_COMMENCED",
			"payload": []any{"not", "an", "object"},
		})
		Expect(status).To(Equal(http.StatusBadRequest))
		Expect(rec.count()).To(Equal(0))
	})
})

// These specs drive the webhook transport for real — a stored subscription, a mutation
// on the HTTP surface, an actual HTTP callback — because it is the one transport whose
// whole delivery path can be stood up in a test without a broker.
var _ = Describe("emulator webhook delivery", func() {
	var (
		baseURL   string
		callbacks *callbackRecorder
	)

	// start brings up an emulator with the real webhook transport, left unwidened: a
	// spec that widened it would have to name a remote URL to prove anything, and a spec
	// suite must not make an outbound request. That path is covered in the unit specs.
	start := func() {
		GinkgoHelper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())

		srv, err := New(Config{})
		Expect(err).NotTo(HaveOccurred())

		go func() { _ = srv.app.Listener(ln) }()
		DeferCleanup(func() { _ = srv.app.Shutdown() })

		baseURL = "http://" + ln.Addr().String()
		waitReady(baseURL)
	}

	BeforeEach(func() {
		callbacks = &callbackRecorder{}
		hook := httptest.NewServer(callbacks)
		DeferCleanup(hook.Close)
		callbacks.url = hook.URL + "/hook"
	})

	It("POSTs the event envelope to a local callbackUrl", func() {
		start()
		registerWebhookSub(baseURL, "ORDER_CREATED", callbacks.url,
			[]any{map[string]any{"key": "X-Test", "value": "1"}})
		createOrder(baseURL, map[string]any{"tenantOrderId": "t"})

		calls := callbacks.calls()
		Expect(calls).To(HaveLen(1))
		Expect(calls[0].contentType).To(Equal("application/json"))
		Expect(calls[0].testHeader).To(Equal("1"))

		var ev webHookEvent
		Expect(json.Unmarshal(calls[0].body, &ev)).To(Succeed())
		Expect(ev.Event).To(Equal("ORDER_CREATED"))
		Expect(ev.EventID).NotTo(BeEmpty())
		Expect(string(ev.Payload)).To(ContainSubstring(`"tenantOrderId":"t"`))
	})

	It("refuses a callbackUrl outside the local network", func() {
		start()
		registerWebhookSub(baseURL, "ORDER_CREATED", "https://example.com/hook", nil)

		status, body := postJSON(baseURL, "/_emulator/emit", map[string]any{
			"event":   "ORDER_CREATED",
			"payload": map[string]any{},
		})
		Expect(status).To(Equal(http.StatusOK))

		var result emitResult
		Expect(json.Unmarshal(body, &result)).To(Succeed())
		Expect(result.Enabled).To(BeTrue())
		Expect(result.Published).To(Equal(0))
	})
})

// callbackRecorder is the subscriber a webhook target points at: it answers 200 and
// records what arrived.
type callbackRecorder struct {
	url string

	mu      sync.Mutex
	records []callbackCall
}

type callbackCall struct {
	body        []byte
	contentType string
	testHeader  string
}

func (c *callbackRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, callbackCall{
		body:        body,
		contentType: r.Header.Get("Content-Type"),
		testHeader:  r.Header.Get("X-Test"),
	})
	w.WriteHeader(http.StatusOK)
}

func (c *callbackRecorder) calls() []callbackCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]callbackCall(nil), c.records...)
}

// registerWebhookSub stores a WEBHOOK subscription through the emulator's own create
// handler, so the specs exercise the same path a user would.
func registerWebhookSub(baseURL, event, callbackURL string, headers []any) {
	GinkgoHelper()
	target := map[string]any{"type": "WEBHOOK", "callbackUrl": callbackURL}
	if headers != nil {
		target["headers"] = headers
	}
	status, _ := postJSON(baseURL, "/api/subscriptions", map[string]any{
		"name": event, "event": event, "target": target,
	})
	Expect(status).To(Equal(http.StatusCreated))
}

// registerPubSubSub stores a GOOGLE_CLOUD_PUB_SUB subscription through the emulator's
// own create handler, so the specs exercise the same path a user would.
func registerPubSubSub(baseURL, event, projectID, topicID string, contexts []any) {
	GinkgoHelper()
	sub := map[string]any{
		"name":   event,
		"event":  event,
		"target": map[string]any{"type": "GOOGLE_CLOUD_PUB_SUB", "projectId": projectID, "topicId": topicID},
	}
	if contexts != nil {
		sub["contexts"] = contexts
	}
	status, _ := postJSON(baseURL, "/api/subscriptions", sub)
	Expect(status).To(Equal(http.StatusCreated))
}
