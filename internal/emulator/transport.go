package emulator

import (
	"context"
	"errors"
	"io"
)

// The three target types SubscriptionForCreation.target is an anyOf over. Each has one
// transport; a target of any other type is skipped.
const (
	targetGoogleCloudPubSub = "GOOGLE_CLOUD_PUB_SUB"
	targetWebhook           = "WEBHOOK"
	targetAzureServiceBus   = "MICROSOFT_AZURE_SERVICE_BUS"
)

// transport delivers an event envelope to one kind of subscription target. A transport
// the user has not configured is absent from the emitter's registry, so a target type
// with no transport is skipped rather than attempted — the same fail-closed shape as
// the read-only gate.
type transport interface {
	// plan resolves a stored target document into a delivery, or reports why it cannot:
	// a malformed target, or one this emulator refuses to reach.
	plan(target map[string]any) (delivery, error)
}

// delivery is one resolved destination: how to reach it, and the label emitResult
// reports it under. The event name is passed to send rather than pre-rendered into
// attributes, because how an event is labelled on the wire is each transport's own
// convention.
type delivery struct {
	label string
	send  func(ctx context.Context, event string, data []byte) error
}

// newTransports builds the registry the emitter delivers through. A test-injected
// registry wins; otherwise each transport is registered only when it can reach
// something. Webhook delivery is always registered because it needs no server of its
// own — it is bounded by *where* it will call (see webhookTransport), not by a flag.
func newTransports(cfg Config) map[string]transport {
	if cfg.transports != nil {
		return cfg.transports
	}

	out := map[string]transport{targetWebhook: newWebhookTransport(cfg.WebhookAllowRemote)}
	if cfg.PubSubHost != "" {
		out[targetGoogleCloudPubSub] = newPubSubTransport(cfg.PubSubHost)
	}
	if cfg.ServiceBusHost != "" {
		out[targetAzureServiceBus] = newServiceBusTransport(cfg.ServiceBusHost)
	}
	return out
}

// closeTransports releases whatever connections the transports hold. Only the ones that
// dial a broker implement io.Closer; the webhook transport holds none.
func closeTransports(transports map[string]transport) error {
	var errs []error
	for _, t := range transports {
		if c, ok := t.(io.Closer); ok {
			errs = append(errs, c.Close())
		}
	}
	return errors.Join(errs...)
}

// subscriptionTarget returns the target a stored subscription delivers to. A
// subscription registered the deprecated way — a top-level callbackUrl and headers and
// no target object, which SubscriptionForCreation still accepts and plenty of live
// subscriptions still use — is read as the webhook target it means.
func subscriptionTarget(sub map[string]any) map[string]any {
	if target := subMap(sub, "target"); target != nil {
		return target
	}

	callbackURL := mapString(sub, "callbackUrl")
	if callbackURL == "" {
		return nil
	}

	target := map[string]any{"type": targetWebhook, "callbackUrl": callbackURL}
	if headers, ok := sub["headers"]; ok {
		target["headers"] = headers
	}
	return target
}
