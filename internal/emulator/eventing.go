package emulator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
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

// eventEmitter turns a domain event into delivered messages, one per stored
// subscription that matches. It reads subscriptions live from the store, so a
// subscription registered a moment ago is honored on the next event.
type eventEmitter struct {
	transports map[string]transport
	store      *Store
	log        io.Writer

	// unavailable is why a target type has no transport, where something more
	// specific than the generic advice is known — a component that is installed and
	// would not start, say. Keyed by target type.
	unavailable map[string]string
}

// newEventEmitter builds the emitter New wires into the handlers.
func newEventEmitter(cfg Config, store *Store) *eventEmitter {
	transports, unavailable := newTransports(cfg)
	return &eventEmitter{transports: transports, unavailable: unavailable, store: store, log: cfg.Log}
}

// enabled reports whether any transport is configured at all. A current emulator always
// has one — webhook delivery needs no broker — so this is false only for an emitter
// built with an empty registry, which a test does and production does not.
func (e *eventEmitter) enabled() bool {
	return len(e.transports) > 0
}

// emitResult reports what emit did: whether eventing is on at all, how many messages
// went out and to which targets. It is the body of the manual emit endpoint's response.
type emitResult struct {
	Enabled   bool     `json:"enabled"`
	Published int      `json:"published"`
	Targets   []string `json:"targets"`

	// Topics carries the same values as Targets under the name it had when Pub/Sub was
	// the only target. It is kept so an older `fft emulator emit` — from a pinned
	// container image, say — still reports where an event went.
	//
	// Deprecated: read Targets.
	Topics []string `json:"topics"`
}

// publishTimeout bounds one emit's whole fan-out, not a single delivery: every matching
// subscription is delivered to under one shared context with this deadline, so a down or
// wrong broker delays the response by at most this long no matter how many subscriptions
// match. Delivery is a side effect of a mutation that has already committed, so it must
// not block the response indefinitely.
const publishTimeout = 10 * time.Second

// emit delivers event to every subscription that names it and whose contexts match
// payload. Delivery is best-effort: one that fails is logged and skipped, never
// propagated, matching the real at-least-once contract where the producer does not
// fail the originating operation on a delivery error.
//
// Matching subscriptions are delivered to concurrently under one bounded context
// detached from any request, not the caller's: an already-committed mutation's event
// must not be cancelled by the caller disconnecting, and one shared deadline caps total
// latency at publishTimeout however many subscriptions match — a dead host delays the
// response by the timeout once, not once per subscription. Delivery is still synchronous
// on the request path, because the manual emit endpoint needs the count.
//
// All matching subscriptions share one eventId, because they are one occurrence of
// the event delivered to several targets — the envelope is built once.
func (e *eventEmitter) emit(event string, payload map[string]any) (result emitResult) {
	// Deferred rather than set at each return, so the deprecated field cannot drift out
	// of step with the one it mirrors.
	defer func() { result.Topics = result.Targets }()

	result = emitResult{Enabled: e.enabled(), Targets: []string{}}
	if !result.Enabled || event == "" {
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

	deliveries := e.plan(event, payload)
	if len(deliveries) == 0 {
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()

	// result is aggregated by the fan-out goroutines, so guard every write to it and
	// the target-dedup map with mu.
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		seen = map[string]bool{}
	)
	for _, d := range deliveries {
		wg.Go(func() {
			// Recovered so a panic inside a transport (not just a returned error) degrades
			// to a logged, best-effort delivery failure instead of crashing the whole
			// emulator process out from under every in-flight request.
			defer func() {
				if r := recover(); r != nil {
					e.logf("emulator: deliver %s to %s panicked: %v", event, d.label, r)
				}
			}()

			if err := d.send(ctx, event, data); err != nil {
				e.logf("emulator: deliver %s to %s: %v", event, d.label, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			result.Published++
			if !seen[d.label] {
				seen[d.label] = true
				result.Targets = append(result.Targets, d.label)
			}
		})
	}
	wg.Wait()
	return result
}

// plan resolves the subscriptions an event reaches into the deliveries to make, in one
// pass so they can then fan out concurrently under a single deadline. Every subscription
// that matched the event but will not be delivered to says why, naming the flag that
// would enable it where there is one: a stored subscription that silently never fires is
// otherwise a mystery, and the emit command points the user here for the reason.
func (e *eventEmitter) plan(event string, payload map[string]any) []delivery {
	var out []delivery
	for _, sub := range e.store.List("subscriptions") {
		if mapString(sub, "event") != event {
			continue
		}
		if !payloadMatchesContexts(payload, subContexts(sub)) {
			continue
		}

		target := subscriptionTarget(sub)
		targetType := mapString(target, "type")
		t, ok := e.transports[targetType]
		if !ok {
			e.logf("emulator: skip %s subscription %q: %s", event, mapString(sub, "name"), e.noTransport(targetType))
			continue
		}

		d, err := t.plan(target)
		if err != nil {
			e.logf("emulator: skip %s subscription %q: %v", event, mapString(sub, "name"), err)
			continue
		}
		out = append(out, d)
	}
	return out
}

// knownTargets are the target types SubscriptionForCreation.target is an anyOf over,
// in the order the startup notice lists them. A subscription may name any of the
// three whether or not anything is installed to deliver it, which is why the notice
// reports all three rather than only the ones that work.
var knownTargets = []string{targetGoogleCloudPubSub, targetWebhook, targetAzureServiceBus}

// enablingComponents names the transport component that delivers a target type, and
// the flag that points it somewhere. The webhook transport is compiled in, so it
// needs neither.
var enablingComponents = map[string]struct{ component, flag string }{
	targetGoogleCloudPubSub: {"emulator-pubsub", "--pubsub-emulator-host"},
	targetAzureServiceBus:   {"emulator-servicebus", "--servicebus-emulator-host"},
}

// unavailableReason explains why a target type has no transport, in the terms that
// let the user fix it.
//
// The fixes are different and saying the wrong one wastes somebody's afternoon, so
// what the emulator actually learned at startup wins over the generic advice: a
// component that is installed and would not start is a different problem from one
// that was never installed, and only the first is worth reading the log for.
func (e *eventEmitter) unavailableReason(targetType string) string {
	if reason := e.unavailable[targetType]; reason != "" {
		return reason
	}

	switch enabling, known := enablingComponents[targetType]; {
	case targetType == "":
		return "it names no target"
	case known:
		return fmt.Sprintf("nothing delivers it (install the %s component, then set %s)",
			enabling.component, enabling.flag)
	default:
		return "no transport delivers it"
	}
}

// offReason is what a target type says when its transport is installed but was not
// pointed anywhere — the ordinary state of a broker you are not using. It names the
// flag that turns it on, and not the install step, because that step is already done.
func offReason(targetType string) string {
	if enabling, known := enablingComponents[targetType]; known {
		return fmt.Sprintf("off (set %s)", enabling.flag)
	}
	return "off (not configured)"
}

// noTransport is [eventEmitter.unavailableReason] for the skip log, where the target
// type has to be named because the line is about one subscription rather than about a
// column of them.
func (e *eventEmitter) noTransport(targetType string) string {
	if targetType == "" {
		return "it names no target"
	}
	return fmt.Sprintf("%s delivery is off: %s", targetType, e.unavailableReason(targetType))
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

// Close releases the resources the transports hold, when they hold any.
func (e *eventEmitter) Close() error {
	return closeTransports(e.transports)
}

func (e *eventEmitter) logf(format string, args ...any) {
	if e.log == nil {
		return
	}
	fmt.Fprintf(e.log, format+"\n", args...)
}

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
