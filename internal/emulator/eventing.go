package emulator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/google/uuid"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// webHookEvent is the envelope fulfillmenttools delivers to a subscriber: the event
// name, a per-occurrence id, and the entity as payload. It carries no tenant,
// timestamp or version of its own — those live inside payload, exactly as the real
// delivery does. eventId is the consumer's dedup key.
type webHookEvent struct {
	Event   string          `json:"event"`
	EventID string          `json:"eventId"`
	Payload json.RawMessage `json:"payload"`
}

// Publisher sends one event to one Pub/Sub topic. It is an interface so a test can
// record what would have been published without standing up a real emulator.
type Publisher interface {
	Publish(ctx context.Context, projectID, topicID string, data []byte, attrs map[string]string) error
}

// pubsubPublisher publishes to a local Pub/Sub emulator over gRPC. Every client it
// builds is pinned to the emulator host with authentication disabled and insecure
// transport, so it can only ever reach that host — the emulator must never publish
// to real Google Cloud. It is constructed only when a host is known.
type pubsubPublisher struct {
	host    string
	mu      sync.Mutex
	clients map[string]*pubsub.Client
}

func newPubsubPublisher(host string) *pubsubPublisher {
	return &pubsubPublisher{host: host, clients: map[string]*pubsub.Client{}}
}

// client returns the cached client for a project, building one on first use. One
// client per project is what the Pub/Sub library wants — a topic is addressed within
// a project, and a subscription can name any project.
func (p *pubsubPublisher) client(ctx context.Context, projectID string) (*pubsub.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.clients[projectID]; ok {
		return c, nil
	}

	c, err := pubsub.NewClient(ctx, projectID,
		option.WithEndpoint(p.host),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		return nil, err
	}
	p.clients[projectID] = c
	return c, nil
}

// Publish creates the topic if the emulator does not have it yet, then publishes the
// message and waits for the emulator to acknowledge it. It stops the publisher
// afterwards so a per-call publisher does not leak a goroutine.
func (p *pubsubPublisher) Publish(ctx context.Context, projectID, topicID string, data []byte, attrs map[string]string) error {
	c, err := p.client(ctx, projectID)
	if err != nil {
		return err
	}

	name := fmt.Sprintf("projects/%s/topics/%s", projectID, topicID)
	if _, err := c.TopicAdminClient.GetTopic(ctx, &pubsubpb.GetTopicRequest{Topic: name}); err != nil {
		if status.Code(err) != codes.NotFound {
			return fmt.Errorf("check topic %s: %w", name, err)
		}
		// AlreadyExists is success, not failure: two events racing to a brand-new topic
		// both see NotFound and both create it, and the loser must not drop its event.
		if _, err := c.TopicAdminClient.CreateTopic(ctx, &pubsubpb.Topic{Name: name}); err != nil && status.Code(err) != codes.AlreadyExists {
			return fmt.Errorf("create topic %s: %w", name, err)
		}
	}

	publisher := c.Publisher(name)
	defer publisher.Stop()

	res := publisher.Publish(ctx, &pubsub.Message{Data: data, Attributes: attrs})
	if _, err := res.Get(ctx); err != nil {
		return fmt.Errorf("publish to %s: %w", name, err)
	}
	return nil
}

// Close closes every cached client and clears the cache. The emulator calls it on
// shutdown so a long-running local session does not leak the gRPC connection and
// background goroutines each client keeps open.
func (p *pubsubPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	for _, c := range p.clients {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	clear(p.clients)
	return errors.Join(errs...)
}

// noopPublisher stands in when no emulator host is configured: subscriptions are
// still stored and matched, but nothing is sent. eventEmitter.enabled is false in
// that case, so this is only ever the zero value's safety net.
type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, string, string, []byte, map[string]string) error {
	return nil
}

// eventEmitter turns a domain event into published messages, one per stored
// subscription that matches. It reads subscriptions live from the store, so a
// subscription registered a moment ago is honored on the next event.
type eventEmitter struct {
	pub     Publisher
	store   *Store
	log     io.Writer
	enabled bool
}

// newEventEmitter builds the emitter New wires into the handlers. A test-injected
// publisher wins; otherwise a real Pub/Sub publisher is built when a host is
// configured, and eventing is disabled (a no-op) when it is not.
func newEventEmitter(cfg Config, store *Store) *eventEmitter {
	switch {
	case cfg.publisher != nil:
		return &eventEmitter{pub: cfg.publisher, store: store, log: cfg.Log, enabled: true}
	case cfg.PubSubHost != "":
		return &eventEmitter{pub: newPubsubPublisher(cfg.PubSubHost), store: store, log: cfg.Log, enabled: true}
	default:
		return &eventEmitter{pub: noopPublisher{}, store: store, log: cfg.Log, enabled: false}
	}
}

// emitResult reports what emit did: whether eventing is on at all, how many messages
// went out and to which project/topic pairs. It is the body of the manual emit
// endpoint's response. Enabled tells a zero Published "no subscription matched" apart
// from "eventing is off", which the caller reports differently.
type emitResult struct {
	Enabled   bool     `json:"enabled"`
	Published int      `json:"published"`
	Topics    []string `json:"topics"`
}

// publishTimeout bounds one emit's whole fan-out, not a single publish: every matching
// subscription is published under one shared context with this deadline, so a down or
// wrong Pub/Sub host delays the response by at most this long no matter how many
// subscriptions match. Delivery is a side effect of a mutation that has already
// committed, so it must not block the response indefinitely.
const publishTimeout = 10 * time.Second

// emit publishes event to every subscription that names it and whose contexts match
// payload. Delivery is best-effort: a publish that fails is logged and skipped, never
// propagated, matching the real at-least-once contract where the producer does not
// fail the originating operation on a delivery error.
//
// Matching subscriptions are published to concurrently under one bounded context
// detached from any request, not the caller's: an already-committed mutation's event
// must not be cancelled by the caller disconnecting, and one shared deadline caps total
// latency at publishTimeout however many subscriptions match — a dead host delays the
// response by the timeout once, not once per subscription. Publishing is still
// synchronous on the request path, because the manual emit endpoint needs the count.
//
// All matching subscriptions share one eventId, because they are one occurrence of
// the event delivered to several targets — the envelope is built once.
func (e *eventEmitter) emit(event string, payload map[string]any) emitResult {
	result := emitResult{Enabled: e.enabled, Topics: []string{}}
	if !e.enabled || event == "" {
		return result
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		e.logf("emulator: encode %s payload: %v", event, err)
		return result
	}
	data, err := json.Marshal(webHookEvent{Event: event, EventID: uuid.NewString(), Payload: raw})
	if err != nil {
		e.logf("emulator: encode %s event: %v", event, err)
		return result
	}

	var matches []subscriptionMatch
	for _, sub := range e.store.List("subscriptions") {
		if mapString(sub, "event") != event {
			continue
		}
		target := subMap(sub, "target")
		if mapString(target, "type") != targetGoogleCloudPubSub {
			continue
		}
		if !payloadMatchesContexts(payload, subContexts(sub)) {
			continue
		}

		projectID, topicID := mapString(target, "projectId"), mapString(target, "topicId")
		if projectID == "" || topicID == "" {
			continue
		}
		matches = append(matches, subscriptionMatch{projectID: projectID, topicID: topicID})
	}
	if len(matches) == 0 {
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()

	// result is aggregated by the fan-out goroutines, so guard every write to it and
	// the topic-dedup map with mu.
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		seen = map[string]bool{}
	)
	for _, m := range matches {
		wg.Go(func() {
			// Recovered so a panic inside Publisher.Publish (not just a returned error)
			// degrades to a logged, best-effort delivery failure instead of crashing the
			// whole emulator process out from under every in-flight request.
			defer func() {
				if r := recover(); r != nil {
					e.logf("emulator: publish %s to %s/%s panicked: %v", event, m.projectID, m.topicID, r)
				}
			}()

			// The event attribute lets a consumer filter without decoding data. It is an
			// emulator convention: fulfillmenttools does not document the attributes its
			// production delivery sets, so nothing here claims to reproduce them.
			if err := e.pub.Publish(ctx, m.projectID, m.topicID, data, map[string]string{"event": event}); err != nil {
				e.logf("emulator: publish %s to %s/%s: %v", event, m.projectID, m.topicID, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			result.Published++
			if topic := m.projectID + "/" + m.topicID; !seen[topic] {
				seen[topic] = true
				result.Topics = append(result.Topics, topic)
			}
		})
	}
	wg.Wait()
	return result
}

// subscriptionMatch is a subscription that emit will publish to: the project and topic
// resolved from its target, collected in a first pass so the publishes can then fan out
// concurrently under one deadline.
type subscriptionMatch struct {
	projectID string
	topicID   string
}

// onCreate, onUpdate and onRemove emit the lifecycle event a collection maps to, if
// any. A collection with no mapping (most of them) emits nothing automatically; the
// manual emit endpoint reaches those.
func (e *eventEmitter) onCreate(coll string, doc map[string]any) {
	e.emit(collectionEvents[coll].created, doc)
}

func (e *eventEmitter) onUpdate(coll string, doc map[string]any) {
	e.emit(collectionEvents[coll].updated, doc)
}

func (e *eventEmitter) onRemove(coll string, doc map[string]any) {
	e.emit(collectionEvents[coll].deleted, doc)
}

// Close releases the underlying publisher's resources when it holds any. The no-op
// and test publishers implement no io.Closer, so Close is nil for them.
func (e *eventEmitter) Close() error {
	if c, ok := e.pub.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (e *eventEmitter) logf(format string, args ...any) {
	if e.log == nil {
		return
	}
	fmt.Fprintf(e.log, format+"\n", args...)
}

// targetGoogleCloudPubSub is the one subscription target type the emulator publishes
// to. A webhook or Azure Service Bus target is stored but skipped.
const targetGoogleCloudPubSub = "GOOGLE_CLOUD_PUB_SUB"

// lifecycleEvents is the event a collection emits on create, update and delete. An
// empty field means the collection has no clean single event for that transition —
// e.g. an order has no delete event, and a pickjob's many state changes do not map to
// a plain PUT — and nothing is emitted for it.
type lifecycleEvents struct {
	created string
	updated string
	deleted string
}

// collectionEvents maps a stateful collection's path segment to its lifecycle events.
// It is deliberately curated to the unambiguous cases: created/updated/deleted whose
// event name is beyond doubt. The long tail of state-transition events
// (PICK_JOB_PICKING_COMMENCED, ROUTING_PLAN_ROUTED, …) is reached through the manual
// emit endpoint, not inferred from CRUD.
var collectionEvents = map[string]lifecycleEvents{
	"facilities":     {created: "FACILITY_CREATED", updated: "FACILITY_UPDATED", deleted: "FACILITY_DELETED"},
	"facilitygroups": {created: "FACILITY_GROUP_CREATED", updated: "FACILITY_GROUP_UPDATED", deleted: "FACILITY_GROUP_DELETED"},
	"users":          {created: "USER_CREATED", updated: "USER_UPDATED", deleted: "USER_DELETED"},
	"orders":         {created: "ORDER_CREATED", updated: "ORDER_MODIFIED"},
	"pickjobs":       {created: "PICK_JOB_CREATED"},
	"packjobs":       {created: "PACK_JOB_CREATED", updated: "PACK_JOB_UPDATED"},
	"handoverjobs":   {created: "HANDOVERJOB_CREATED"},
	"shipments":      {created: "SHIPMENT_CREATED", updated: "SHIPMENT_UPDATED"},
	"itemreturnjobs": {created: "ITEM_RETURN_JOB_CREATED", updated: "ITEM_RETURN_JOB_UPDATED"},
	"stowjobs":       {created: "STOW_JOB_CREATED"},
	"servicejobs":    {created: "SERVICE_JOB_CREATED"},
}

// subscriptionContext is one AND-combined filter on a subscription: an event is
// delivered only when, for every context, at least one of its values names a location
// the entity references.
type subscriptionContext struct {
	values []string
}

// subContexts pulls the contexts out of a stored subscription document. A missing or
// malformed contexts array is no contexts — an unfiltered subscription.
func subContexts(sub map[string]any) []subscriptionContext {
	raw, ok := sub["contexts"].([]any)
	if !ok {
		return nil
	}

	var out []subscriptionContext
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		c := subscriptionContext{}
		if vals, ok := m["values"].([]any); ok {
			for _, v := range vals {
				if s, ok := v.(string); ok {
					c.values = append(c.values, s)
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// payloadMatchesContexts reports whether an entity satisfies a subscription's
// contexts. No contexts always matches. Otherwise every context must be satisfied by
// at least one of its values naming a location the payload references.
//
// The match is best-effort: it scans the payload for the location-reference fields
// entities use (facilityRef/facilityId/tenantFacilityId and their group equivalents),
// accepting a urn:fft:facility:...:<id> as its bare id too. A context's declared type
// is not distinguished — a value is matched against all location references found,
// whether the context is FACILITY or FACILITY_GROUP — and facility groups are not
// resolved to their member facilities, so a FACILITY_GROUP context matches only when
// the entity names that group directly.
func payloadMatchesContexts(payload map[string]any, contexts []subscriptionContext) bool {
	if len(contexts) == 0 {
		return true
	}

	refs := map[string]struct{}{}
	collectLocationRefs(payload, refs)

	for _, ctx := range contexts {
		matched := false
		for _, v := range ctx.values {
			if _, ok := refs[v]; ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// locationKeys are the fields an entity carries a facility or facility-group
// reference under. The emulator matches a subscription context against the values
// found beneath them.
var locationKeys = map[string]bool{
	"facilityRef":      true,
	"facilityId":       true,
	"tenantFacilityId": true,
	"facilityGroupRef": true,
	"facilityGroupId":  true,
}

// collectLocationRefs walks a decoded document and records every location reference
// it finds into refs, adding both the raw value and, for a URN, its bare id.
func collectLocationRefs(v any, refs map[string]struct{}) {
	switch t := v.(type) {
	case map[string]any:
		for key, val := range t {
			if s, ok := val.(string); ok && locationKeys[key] {
				refs[s] = struct{}{}
				if _, id, ok := parseURN(s); ok {
					refs[id] = struct{}{}
				}
			}
			collectLocationRefs(val, refs)
		}
	case []any:
		for _, item := range t {
			collectLocationRefs(item, refs)
		}
	}
}

// mapString reads a string field from a decoded document, "" when absent or not a
// string.
func mapString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// subMap reads a nested object from a decoded document, nil when absent or not an
// object.
func subMap(m map[string]any, key string) map[string]any {
	sub, _ := m[key].(map[string]any)
	return sub
}
