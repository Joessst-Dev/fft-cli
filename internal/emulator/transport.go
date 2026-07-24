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

		// The manifest declared what it delivers and the handshake confirmed it. Where
		// they disagree the handshake wins, because it is the running code — but the
		// disagreement is worth saying out loud, since the manifest is what the user read
		// before installing it.
		for _, target := range t.targets {
			if !c.Delivers(target) {
				logf(cfg.Log, "emulator: the %s transport delivers %s, which its manifest does not declare", c.Name, target)
			}
			// Two transports claiming one target: keep the first and close the second,
			// rather than overwrite the map entry and leak the first's child process for
			// the life of the emulator. First-installed wins is arbitrary but stable, and
			// the collision is worth a line — the user installed two things that fight.
			if _, taken := out[target]; taken {
				logf(cfg.Log, "emulator: %s also delivers %s, which another transport already does; ignoring it", c.Name, target)
				_ = t.Close()
				break
			}
			out[target] = t
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
