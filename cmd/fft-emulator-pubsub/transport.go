package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// pubSubTransport publishes to a local Pub/Sub emulator over gRPC. Every client it
// builds is pinned to the emulator host with authentication disabled and insecure
// transport, so it can only ever reach that host — the emulator must never publish to
// real Google Cloud. It is constructed only when a host is known.
type pubSubTransport struct {
	host    string
	mu      sync.Mutex
	closed  bool
	clients map[string]*pubsub.Client
}

func newPubSubTransport(host string) *pubSubTransport {
	return &pubSubTransport{host: host, clients: map[string]*pubsub.Client{}}
}

// Hello reports what this transport delivers and where.
func (t *pubSubTransport) Hello() ([]string, string, error) {
	return []string{targetGoogleCloudPubSub},
		fmt.Sprintf("publishing to the Pub/Sub emulator at %s", t.host), nil
}

// Plan resolves a SubscriptionTargetGoogleCloudPubSub into the topic to publish to.
func (t *pubSubTransport) Plan(target map[string]any) (string, error) {
	projectID, topicID, err := topicOf(target)
	if err != nil {
		return "", err
	}
	return projectID + "/" + topicID, nil
}

// Send publishes one event. The target is resolved again rather than captured at Plan
// time: the protocol carries it on every frame, so the transport keeps no state
// between them and has no handles to leak.
func (t *pubSubTransport) Send(ctx context.Context, target map[string]any, event string, data []byte) error {
	projectID, topicID, err := topicOf(target)
	if err != nil {
		return err
	}

	// The event attribute lets a consumer filter without decoding data. It is an
	// emulator convention: fulfillmenttools does not document the attributes its
	// production delivery sets, so nothing here claims to reproduce them.
	return t.publish(ctx, projectID, topicID, data, map[string]string{"event": event})
}

// topicOf reads the project and topic a target addresses.
func topicOf(target map[string]any) (projectID, topicID string, err error) {
	projectID, topicID = mapString(target, "projectId"), mapString(target, "topicId")
	if projectID == "" || topicID == "" {
		return "", "", errors.New("target names no projectId and topicId")
	}
	return projectID, topicID, nil
}

// client returns the cached client for a project, building one on first use. One client
// per project is what the Pub/Sub library wants — a topic is addressed within a project,
// and a subscription can name any project.
func (t *pubSubTransport) client(ctx context.Context, projectID string) (*pubsub.Client, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Shutdown races an in-flight fan-out — the server's drain is shorter than an emit's
	// deadline — and a client built after Close is one nothing will ever close.
	if t.closed {
		return nil, errors.New("the Pub/Sub transport is closed")
	}
	if c, ok := t.clients[projectID]; ok {
		return c, nil
	}

	c, err := pubsub.NewClient(ctx, projectID,
		option.WithEndpoint(t.host),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		return nil, err
	}
	t.clients[projectID] = c
	return c, nil
}

// publish creates the topic if the emulator does not have it yet, then publishes the
// message and waits for the emulator to acknowledge it. It stops the publisher
// afterwards so a per-call publisher does not leak a goroutine.
func (t *pubSubTransport) publish(ctx context.Context, projectID, topicID string, data []byte, attrs map[string]string) error {
	c, err := t.client(ctx, projectID)
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

// Close closes every cached client and clears the cache. It runs when the emulator
// closes this process's stdin, so a long-running local session does not leak the gRPC
// connection and the background goroutines each client keeps open.
func (t *pubSubTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true

	var errs []error
	for _, c := range t.clients {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	clear(t.clients)
	return errors.Join(errs...)
}
