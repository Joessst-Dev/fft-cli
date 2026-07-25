package emulator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Joessst-Dev/fft-cli/internal/component"
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

// describer is a transport that can say where it delivers, for the startup notice.
// A transport that cannot is reported by its target type alone.
type describer interface{ describe() string }

// newTransports builds the registry the emitter delivers through.
//
// Two sources, and only two. Webhook delivery is compiled in, because it needs no
// broker and nothing but net/http — it is bounded by *where* it will call (see
// webhookTransport), not by whether anything was installed. Everything else comes
// from a transport component: a separate process that answers the line protocol, so
// that a broker fft has never heard of is somebody else's binary rather than a
// dependency in everybody's.
//
// A component that will not start is logged and left out, not fatal. One broken
// transport must not stop an emulator whose other targets work — and the startup
// notice will say the target type is unavailable, which is the honest report.
func newTransports(cfg Config) (map[string]transport, map[string]string) {
	if cfg.transports != nil {
		return cfg.transports, nil
	}

	out := map[string]transport{targetWebhook: newWebhookTransport(cfg.WebhookAllowRemote)}
	reasons := map[string]string{}

	for _, c := range cfg.Components.Transports() {
		// A transport that declares configuration it has not been given is off, not
		// broken. Starting it just to watch it refuse at the handshake would turn "you
		// did not set --pubsub-emulator-host" — the ordinary case for a target you are
		// not using — into a line that reads like a fault. So it is left unstarted, with
		// the reason that names the flag.
		if unconfigured(c, cfg.TransportEnv) {
			for _, target := range c.Targets {
				reasons[target] = offReason(target)
			}
			continue
		}

		t, err := newProcessTransport(c, cfg.TransportEnv, cfg.Log)
		if err != nil {
			logf(cfg.Log, "emulator: %v", err)

			// Remembered, so the startup notice can tell a component that is not installed
			// from one that is installed and would not start. They have completely
			// different fixes, and a notice that guesses wrong sends somebody to install
			// what they already have.
			for _, target := range c.Targets {
				reasons[target] = fmt.Sprintf(
					"the %s component is installed but did not start (see the log above)", c.Name)
			}
			continue
		}

		// A transport delivers only what its manifest declared — the list the user read
		// and agreed to before installing it. The handshake confirms that list; it does
		// not get to add to it. A target the running code claims but the manifest does
		// not is refused, not merely logged, so a misbehaving or reordered component
		// cannot claim a target type ahead of the one that actually declared it.
		//
		// Register the targets this transport wins, and skip the ones another already
		// serves. It is kept alive if it registered anything and closed only if it won
		// nothing — closing it while it still holds a target another loop iteration
		// registered under it would leave a live-looking target pointed at a dead child.
		registered := 0
		for _, target := range t.targets {
			if !c.Delivers(target) {
				logf(cfg.Log, "emulator: the %s transport claims %s, which its manifest does not declare; ignoring it", c.Name, target)
				continue
			}
			if _, taken := out[target]; taken {
				logf(cfg.Log, "emulator: %s also delivers %s, which another transport already does; ignoring it", c.Name, target)
				continue
			}
			out[target] = t
			registered++
		}

		// A transport that greeted successfully but delivers nothing new — every target
		// it claimed was already served, or it answered hello with no targets at all — is
		// a child that would otherwise run untouched for the emulator's whole life. Close
		// it: nothing routes to it.
		if registered == 0 {
			logf(cfg.Log, "emulator: the %s transport registered no targets; stopping it", c.Name)
			_ = t.Close()
		}
	}
	return out, reasons
}

// unconfigured reports whether a transport declares configuration it has not been
// given.
//
// The manifest's env list is what a transport reads to know where to deliver — the
// Pub/Sub transport reads PUBSUB_EMULATOR_HOST, set from --pubsub-emulator-host. A
// transport that declares such a variable and was handed no value for any of them is
// one the user did not point anywhere, so there is nothing to start. A transport that
// declares no configuration at all is always started: it needs none.
//
// A value counts whether it came from the emulator's own flags (env, e.g.
// --pubsub-emulator-host) or from the ambient environment the child will inherit. The
// second is what makes a community transport work at all: fft has no flag for a Kafka
// broker, so a Kafka transport reading KAFKA_BROKER from the environment would
// otherwise always look unconfigured and never start.
func unconfigured(c component.Component, env map[string]string) bool {
	if len(c.Env) == 0 {
		return false
	}
	for _, name := range c.Env {
		if env[name] != "" || os.Getenv(name) != "" {
			return false
		}
	}
	return true
}

// logf writes one line to the emulator's log, if there is one.
func logf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format+"\n", args...)
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
