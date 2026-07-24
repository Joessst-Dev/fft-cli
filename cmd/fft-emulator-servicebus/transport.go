package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
)

// serviceBusTransport sends the event envelope to a queue or topic on a local Azure
// Service Bus emulator.
//
// It is configured with a host rather than a connection string on purpose, which is the
// same pin the Pub/Sub transport has. What that buys is not that the host must be local —
// the SDK accepts any hostname under UseDevelopmentEmulator, and the compose recipe in
// the docs relies on that — but that the *credentials* are fixed: emulator mode speaks
// plain AMQP with TLS disabled and the emulator's published root key, which no real Azure
// namespace will accept. Taking a connection string instead would hand that back and let
// the emulator be pointed at a live namespace.
//
// The target's tenantId, clientId and clientSecret are ignored: they are Microsoft Entra
// credentials, and no local emulator honours Entra auth.
type serviceBusTransport struct {
	host    string
	mu      sync.Mutex
	closed  bool
	client  *azservicebus.Client
	senders map[string]*azservicebus.Sender
}

func newServiceBusTransport(host string) *serviceBusTransport {
	return &serviceBusTransport{host: host, senders: map[string]*azservicebus.Sender{}}
}

// developmentConnectionString is the shape Microsoft's Service Bus emulator accepts:
// the fixed root key, and UseDevelopmentEmulator, which is what tells the SDK to speak
// plain AMQP to the given host instead of AMQPS to Azure.
const developmentConnectionString = "Endpoint=sb://%s;SharedAccessKeyName=RootManageSharedAccessKey;" +
	"SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true;"

// Hello reports what this transport delivers and where.
func (t *serviceBusTransport) Hello() ([]string, string, error) {
	return []string{targetAzureServiceBus},
		fmt.Sprintf("sending to the Service Bus emulator at %s", t.host), nil
}

// Plan resolves a SubscriptionTargetMicrosoftAzureServiceBus into the entity to send to.
// The namespace is only a label — the local emulator serves one namespace, which the
// host already names — but the queue or topic is what the send addresses.
func (t *serviceBusTransport) Plan(target map[string]any) (string, error) {
	entity, err := entityOf(target)
	if err != nil {
		return "", err
	}

	if namespace := mapString(target, "namespace"); namespace != "" {
		return namespace + "/" + entity, nil
	}
	return entity, nil
}

// Send delivers one event to the entity the target names. The target is resolved
// again rather than captured at Plan time: the protocol carries it on every frame, so
// the transport keeps no state between them and has no handles to leak.
func (t *serviceBusTransport) Send(ctx context.Context, target map[string]any, event string, data []byte) error {
	entity, err := entityOf(target)
	if err != nil {
		return err
	}
	return t.send(ctx, entity, event, data)
}

// entityOf reads the queue or topic a target addresses.
func entityOf(target map[string]any) (string, error) {
	entity := mapString(target, "queueOrTopicName")
	if entity == "" {
		return "", errors.New("target names no queueOrTopicName")
	}
	return entity, nil
}

// send delivers one message. The event application property is the counterpart of the
// Pub/Sub message attribute — an emulator convention that lets a consumer filter without
// decoding the body, not a claim about what production sets.
func (t *serviceBusTransport) send(ctx context.Context, entity, event string, data []byte) error {
	sender, err := t.sender(entity)
	if err != nil {
		return err
	}

	contentType := "application/json"
	msg := &azservicebus.Message{
		Body:                  data,
		ContentType:           &contentType,
		ApplicationProperties: map[string]any{"event": event},
	}
	if err := sender.SendMessage(ctx, msg, nil); err != nil {
		// The emulator serves only the queues and topics its config file declares and
		// cannot create one on demand, so a missing entity is the likeliest cause here
		// and worth naming rather than leaving to a bare AMQP error.
		return fmt.Errorf("send to %q (is it declared in the emulator's config?): %w", entity, err)
	}
	return nil
}

// sender returns the cached sender for an entity, building the client and the sender on
// first use. A Sender is safe for concurrent use, so one per entity is enough for the
// whole fan-out.
func (t *serviceBusTransport) sender(entity string) (*azservicebus.Sender, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Shutdown races an in-flight fan-out — the server's drain is shorter than an emit's
	// deadline — and a sender built after Close is one nothing will ever close.
	if t.closed {
		return nil, errors.New("the Service Bus transport is closed")
	}
	if s, ok := t.senders[entity]; ok {
		return s, nil
	}

	if t.client == nil {
		// The host is interpolated into a ';'-delimited property list, so a host carrying
		// a ';' would smuggle in properties of its own — including credentials that would
		// undo the pin above.
		if strings.Contains(t.host, ";") {
			return nil, fmt.Errorf("the Service Bus emulator host %q must not contain ';'", t.host)
		}
		client, err := azservicebus.NewClientFromConnectionString(
			fmt.Sprintf(developmentConnectionString, t.host), nil)
		if err != nil {
			return nil, fmt.Errorf("reach the Service Bus emulator at %s: %w", t.host, err)
		}
		t.client = client
	}

	s, err := t.client.NewSender(entity, nil)
	if err != nil {
		return nil, fmt.Errorf("open a sender for %q: %w", entity, err)
	}
	t.senders[entity] = s
	return s, nil
}

// closeGrace bounds the AMQP shutdown handshake, so a shutdown is not held up by a
// broker that has already gone away.
const closeGrace = 5 * time.Second

// Close closes every sender and the client. It runs when the emulator closes this
// process's stdin, so a long-running local session does not leak the AMQP connection.
func (t *serviceBusTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true

	ctx, cancel := context.WithTimeout(context.Background(), closeGrace)
	defer cancel()

	var errs []error
	for _, s := range t.senders {
		errs = append(errs, s.Close(ctx))
	}
	clear(t.senders)

	if t.client != nil {
		errs = append(errs, t.client.Close(ctx))
		t.client = nil
	}
	return errors.Join(errs...)
}
