package emulator

// These specs drive event publishing over a real socket, with a recording publisher
// in place of a real Pub/Sub emulator. They prove the wiring the unit tests cannot:
// that a CRUD mutation on the HTTP surface reaches the emitter, that the subscription
// store the create handler wrote is the one the emitter reads, and that the manual
// /_emulator/emit route publishes.

import (
	"encoding/json"
	"net"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("emulator eventing over HTTP", func() {
	var (
		baseURL string
		rec     *recordingPublisher
	)

	BeforeEach(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())

		rec = &recordingPublisher{}
		srv, err := New(Config{publisher: rec})
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
		Expect(msgs[0].topicID).To(Equal("orders"))

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
		Expect(rec.messages()[0].topicID).To(Equal("pick"))
	})

	It("rejects an emit with no event name", func() {
		status, _ := postJSON(baseURL, "/_emulator/emit", map[string]any{"payload": map[string]any{}})
		Expect(status).To(Equal(http.StatusBadRequest))
	})
})

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
