package emulator

import (
	"context"
	"encoding/json"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// recordingPublisher stands in for a real Pub/Sub emulator: it records what would
// have been published instead of sending it. It is safe to read from a test after the
// handler goroutine that published has returned.
type recordingPublisher struct {
	mu    sync.Mutex
	calls []publishedMessage
}

type publishedMessage struct {
	projectID string
	topicID   string
	data      []byte
	attrs     map[string]string
}

func (p *recordingPublisher) Publish(_ context.Context, projectID, topicID string, data []byte, attrs map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, publishedMessage{projectID, topicID, data, attrs})
	return nil
}

func (p *recordingPublisher) messages() []publishedMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]publishedMessage(nil), p.calls...)
}

func (p *recordingPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
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
		rec   *recordingPublisher
		emit  *eventEmitter
	)

	BeforeEach(func() {
		store = NewStore(map[string]collectionMeta{})
		rec = &recordingPublisher{}
		emit = &eventEmitter{pub: rec, store: store, enabled: true}
	})

	Describe("emit", func() {
		It("publishes to a matching subscription's topic with the event envelope", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "orders", nil))

			result := emit.emit("ORDER_CREATED", map[string]any{"tenantOrderId": "t"})

			Expect(result.Enabled).To(BeTrue())
			Expect(result.Published).To(Equal(1))
			Expect(result.Topics).To(ConsistOf("local/orders"))

			msgs := rec.messages()
			Expect(msgs).To(HaveLen(1))
			Expect(msgs[0].projectID).To(Equal("local"))
			Expect(msgs[0].topicID).To(Equal("orders"))
			Expect(msgs[0].attrs).To(HaveKeyWithValue("event", "ORDER_CREATED"))

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
			Expect(result.Topics).To(ConsistOf("local/orders"))
			Expect(rec.count()).To(Equal(2))
		})

		It("skips a subscription registered for a different event", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_MODIFIED", "local", "orders", nil))

			result := emit.emit("ORDER_CREATED", map[string]any{})
			Expect(result.Published).To(Equal(0))
			Expect(rec.count()).To(Equal(0))
		})

		It("skips a target that is not a Pub/Sub topic", func() {
			store.Create("subscriptions", entityDoc{
				"event":  "ORDER_CREATED",
				"target": map[string]any{"type": "WEBHOOK", "callbackUrl": "https://example.test/hook"},
			})

			result := emit.emit("ORDER_CREATED", map[string]any{})
			Expect(result.Published).To(Equal(0))
		})

		It("does nothing when eventing is disabled", func() {
			store.Create("subscriptions", pubSubSubscription("ORDER_CREATED", "local", "orders", nil))
			emit.enabled = false

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
	})

	Describe("Close", func() {
		It("is a no-op when the publisher holds no closable resources", func() {
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

// lastEvent decodes the event name of the most recent published message.
func lastEvent(rec *recordingPublisher) string {
	GinkgoHelper()
	msgs := rec.messages()
	Expect(msgs).NotTo(BeEmpty())
	var ev webHookEvent
	Expect(json.Unmarshal(msgs[len(msgs)-1].data, &ev)).To(Succeed())
	return ev.Event
}
