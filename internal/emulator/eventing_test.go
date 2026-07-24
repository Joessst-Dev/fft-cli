package emulator

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// recordingTransport stands in for a real broker or callback endpoint: it records what
// would have been delivered instead of sending it, labelling each destination the way
// the transport it replaces does. It is safe to read from a test after the handler
// goroutine that delivered has returned.
type recordingTransport struct {
	label func(target map[string]any) string
	mu    sync.Mutex
	calls []deliveredMessage
}

type deliveredMessage struct {
	label string
	event string
	data  []byte
}

// recordingPubSub and recordingWebhook are the two target types the specs below drive,
// each labelling a destination as its real transport would.
func recordingPubSub() *recordingTransport {
	return &recordingTransport{label: func(t map[string]any) string {
		return mapString(t, "projectId") + "/" + mapString(t, "topicId")
	}}
}

func recordingWebhook() *recordingTransport {
	return &recordingTransport{label: func(t map[string]any) string { return mapString(t, "callbackUrl") }}
}

func (r *recordingTransport) plan(target map[string]any) (delivery, error) {
	label := r.label(target)
	return delivery{label: label, send: func(_ context.Context, event string, data []byte) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.calls = append(r.calls, deliveredMessage{label, event, data})
		return nil
	}}, nil
}

func (r *recordingTransport) messages() []deliveredMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]deliveredMessage(nil), r.calls...)
}

func (r *recordingTransport) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// panickingTransport stands in for a transport bug: it panics instead of returning an
// error, so a spec can assert emit's fan-out recovers rather than crashing the process.
type panickingTransport struct{}

func (panickingTransport) plan(map[string]any) (delivery, error) {
	return delivery{label: "boom", send: func(context.Context, string, []byte) error { panic("boom") }}, nil
}

// pubSubSubscription builds the stored shape of a GOOGLE_CLOUD_PUB_SUB subscription.
func pubSubSubscription(event, projectID, topicID string, contexts []any) entityDoc {
	sub := entityDoc{
		"name":   event,
		"event":  event,
		"target": map[string]any{"type": targetGoogleCloudPubSub, "projectId": projectID, "topicId": topicID},
	}
	if contexts != nil {
		sub["contexts"] = contexts
	}
	return sub
}

var _ = Describe("eventEmitter", func() {
	var (
		store *Store
		rec   *recordingTransport
		hooks *recordingTransport
		emit  *eventEmitter
	)

	BeforeEach(func() {
		store = NewStore(map[string]collectionMeta{})
		rec, hooks = recordingPubSub(), recordingWebhook()
		emit = &eventEmitter{
			transports: map[string]transport{targetGoogleCloudPubSub: rec, targetWebhook: hooks},
			store:      store,
		}
	})

	Describe("emit", func() {
		It("publishes to a matching subscription's topic with the event envelope", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "orders", nil))

			result := emit.emit("ORDER_CREATED", map[string]any{"tenantOrderId": "t"})

			Expect(result.Enabled).To(BeTrue())
			Expect(result.Published).To(Equal(1))
			Expect(result.Targets).To(ConsistOf("local/orders"))
			// Topics is the old name of Targets, kept for an older emit command.
			Expect(result.Topics).To(Equal(result.Targets))

			msgs := rec.messages()
			Expect(msgs).To(HaveLen(1))
			Expect(msgs[0].label).To(Equal("local/orders"))
			Expect(msgs[0].event).To(Equal("ORDER_CREATED"))

			var ev webHookEvent
			Expect(json.Unmarshal(msgs[0].data, &ev)).To(Succeed())
			Expect(ev.Event).To(Equal("ORDER_CREATED"))
			Expect(ev.EventID).NotTo(BeEmpty())
			Expect(string(ev.Payload)).To(ContainSubstring(`"tenantOrderId":"t"`))
		})

		It("delivers one occurrence to several subscriptions under a single eventId", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "a", nil))
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "b", nil))

			result := emit.emit("ORDER_CREATED", map[string]any{"tenantOrderId": "t"})
			Expect(result.Published).To(Equal(2))

			msgs := rec.messages()
			Expect(msgs).To(HaveLen(2))

			var first, second webHookEvent
			Expect(json.Unmarshal(msgs[0].data, &first)).To(Succeed())
			Expect(json.Unmarshal(msgs[1].data, &second)).To(Succeed())
			Expect(first.EventID).To(Equal(second.EventID))
		})

		It("counts each message but lists a shared topic only once", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "orders", nil))
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "orders", nil))

			result := emit.emit("ORDER_CREATED", map[string]any{"tenantOrderId": "t"})
			Expect(result.Published).To(Equal(2))
			Expect(result.Targets).To(ConsistOf("local/orders"))
			Expect(rec.count()).To(Equal(2))
		})

		It("skips a subscription registered for a different event", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_MODIFIED", "local", "orders", nil))

			result := emit.emit("ORDER_CREATED", map[string]any{})
			Expect(result.Published).To(Equal(0))
			Expect(rec.count()).To(Equal(0))
		})

		It("delivers a webhook target through its own transport", func() {
			store.Create("subscriptions", entityDoc{
				"event":  "ORDER_CREATED",
				"target": map[string]any{"type": targetWebhook, "callbackUrl": "http://localhost:3000/hook"},
			})

			result := emit.emit("ORDER_CREATED", map[string]any{})
			Expect(result.Published).To(Equal(1))
			Expect(result.Targets).To(ConsistOf("http://localhost:3000/hook"))
			Expect(hooks.count()).To(Equal(1))
			Expect(rec.count()).To(Equal(0))
		})

		It("reads a subscription's deprecated top-level callbackUrl as a webhook target", func() {
			store.Create("subscriptions", entityDoc{
				"event":       "ORDER_CREATED",
				"callbackUrl": "http://localhost:3000/legacy",
			})

			result := emit.emit("ORDER_CREATED", map[string]any{})
			Expect(result.Targets).To(ConsistOf("http://localhost:3000/legacy"))
		})

		It("delivers one occurrence across target types under a single eventId", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "orders", nil))
			store.Create("subscriptions", entityDoc{
				"event":  "ORDER_CREATED",
				"target": map[string]any{"type": targetWebhook, "callbackUrl": "http://localhost:3000/hook"},
			})

			result := emit.emit("ORDER_CREATED", map[string]any{})
			Expect(result.Published).To(Equal(2))
			Expect(result.Targets).To(ConsistOf("local/orders", "http://localhost:3000/hook"))

			var published, called webHookEvent
			Expect(json.Unmarshal(rec.messages()[0].data, &published)).To(Succeed())
			Expect(json.Unmarshal(hooks.messages()[0].data, &called)).To(Succeed())
			Expect(published.EventID).To(Equal(called.EventID))
		})

		It("skips a target type no transport is configured for, naming the flag that would enable it", func() {
			var log bytes.Buffer
			emit.log = &log
			store.Create("subscriptions", entityDoc{
				"name":  "orders",
				"event": "ORDER_CREATED",
				"target": map[string]any{
					"type": targetAzureServiceBus, "namespace": "ns", "queueOrTopicName": "orders",
				},
			})

			result := emit.emit("ORDER_CREATED", map[string]any{})
			Expect(result.Published).To(Equal(0))
			Expect(log.String()).To(ContainSubstring("--servicebus-emulator-host"))
		})

		It("does nothing when no transport is configured at all", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "orders", nil))
			emit.transports = nil

			result := emit.emit("ORDER_CREATED", map[string]any{})
			Expect(result.Enabled).To(BeFalse())
			Expect(result.Published).To(Equal(0))
			Expect(rec.count()).To(Equal(0))
		})

		It("reports eventing enabled even when no subscription matches", func() {
			result := emit.emit("ORDER_CREATED", map[string]any{})
			Expect(result.Enabled).To(BeTrue())
			Expect(result.Published).To(Equal(0))
		})

		It("does nothing for an empty event name", func() {
			store.Create("subscriptions", pubSubSubscription("", "local", "orders", nil))
			Expect(emit.emit("", map[string]any{}).Published).To(Equal(0))
		})

		It("recovers from a panic in a transport instead of crashing the fan-out", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "orders", nil))
			emit.transports[targetGoogleCloudPubSub] = panickingTransport{}

			var result emitResult
			Expect(func() { result = emit.emit("ORDER_CREATED", map[string]any{"tenantOrderId": "t"}) }).NotTo(Panic())
			Expect(result.Published).To(Equal(0))
		})
	})

	Describe("Close", func() {
		It("is a no-op when no transport holds a closable resource", func() {
			Expect(emit.Close()).To(Succeed())
		})
	})

	Describe("lifecycle mapping", func() {
		BeforeEach(func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "orders", nil))
			store.Create("subscriptions", pubSubSubscription("ORDER_MODIFIED", "local", "orders", nil))
			store.Create("subscriptions", pubSubSubscription("FACILITY_DELETED", "local", "facilities", nil))
		})

		It("maps a create to the collection's created event", func() {
			emit.onCreate("orders", map[string]any{"tenantOrderId": "t"})
			Expect(lastEvent(rec)).To(Equal("ORDER_CREATED"))
		})

		It("maps an update to the collection's updated event", func() {
			emit.onUpdate("orders", map[string]any{"tenantOrderId": "t"})
			Expect(lastEvent(rec)).To(Equal("ORDER_MODIFIED"))
		})

		It("emits the deleted event for a collection that has one", func() {
			emit.onRemove("facilities", map[string]any{"id": "f1"})
			Expect(lastEvent(rec)).To(Equal("FACILITY_DELETED"))
		})

		It("emits nothing for a transition a collection does not map", func() {
			// Orders have no delete event.
			emit.onRemove("orders", map[string]any{"id": "o1"})
			Expect(rec.count()).To(Equal(0))
		})

		It("emits nothing for a collection with no mapping at all", func() {
			emit.onCreate("carriers", map[string]any{"id": "c1"})
			Expect(rec.count()).To(Equal(0))
		})
	})
})

var _ = Describe("payloadMatchesContexts", func() {
	facilityContext := func(values ...string) []subscriptionContext {
		return []subscriptionContext{{values: values}}
	}

	It("matches when there are no contexts", func() {
		Expect(payloadMatchesContexts(map[string]any{}, nil)).To(BeTrue())
	})

	It("matches a facility the entity references directly", func() {
		payload := map[string]any{"facilityRef": "BER-01"}
		Expect(payloadMatchesContexts(payload, facilityContext("BER-01"))).To(BeTrue())
	})

	It("does not match a facility the entity does not reference", func() {
		payload := map[string]any{"facilityRef": "HAM-02"}
		Expect(payloadMatchesContexts(payload, facilityContext("BER-01"))).To(BeFalse())
	})

	It("matches a facility referenced as a URN against its bare id", func() {
		payload := map[string]any{"facilityRef": "urn:fft:facility:tenantFacilityId:BER-01"}
		Expect(payloadMatchesContexts(payload, facilityContext("BER-01"))).To(BeTrue())
	})

	It("finds a facility reference nested inside the entity", func() {
		payload := map[string]any{
			"pick": map[string]any{"facilityId": "BER-01"},
		}
		Expect(payloadMatchesContexts(payload, facilityContext("BER-01"))).To(BeTrue())
	})

	It("requires every context to match, since contexts are AND-combined", func() {
		payload := map[string]any{"facilityRef": "BER-01"}
		contexts := []subscriptionContext{{values: []string{"BER-01"}}, {values: []string{"HAM-02"}}}
		Expect(payloadMatchesContexts(payload, contexts)).To(BeFalse())
	})
})

// lastEvent decodes the event name of the most recent delivered message.
func lastEvent(rec *recordingTransport) string {
	GinkgoHelper()
	msgs := rec.messages()
	Expect(msgs).NotTo(BeEmpty())
	var ev webHookEvent
	Expect(json.Unmarshal(msgs[len(msgs)-1].data, &ev)).To(Succeed())
	return ev.Event
}
