package emulator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// webhookTransport POSTs the event envelope to a subscription's callbackUrl.
//
// It needs no server of its own, so unlike the broker transports it is always
// registered. What bounds it is where it will call: a callbackUrl is arbitrary user
// data, and a subscription fixture copied from a real tenant names a real endpoint —
// which the emulator must not fire made-up events at. So a target outside the local
// network is refused unless the user widened it deliberately, the same bargain as
// binding --host 0.0.0.0.
type webhookTransport struct {
	client      *http.Client
	allowRemote bool
}

func newWebhookTransport(allowRemote bool) *webhookTransport {
	return &webhookTransport{
		// Redirects are not followed: a local endpoint answering 302 with a remote
		// Location would walk the request straight past the check below. Returning the
		// redirect as the response instead makes it a non-2xx, i.e. a logged failure.
		client: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		allowRemote: allowRemote,
	}
}

// plan resolves a SubscriptionTargetWebhook into the endpoint to POST to, refusing a
// callbackUrl that is unusable or that this emulator will not reach.
func (t *webhookTransport) plan(target map[string]any) (delivery, error) {
	raw := mapString(target, "callbackUrl")
	if raw == "" {
		return delivery{}, errors.New("target names no callbackUrl")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return delivery{}, fmt.Errorf("callbackUrl %q is not a URL: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return delivery{}, fmt.Errorf("callbackUrl %q is not http or https", raw)
	}
	if u.Host == "" {
		return delivery{}, fmt.Errorf("callbackUrl %q names no host", raw)
	}
	if !t.allowRemote && !isLocalHost(u.Hostname()) {
		return delivery{}, fmt.Errorf("callbackUrl %q is not a local host (--webhook-allow-remote to call it anyway)", raw)
	}

	headers := callbackHeaders(target)
	return delivery{
		label: raw,
		send: func(ctx context.Context, _ string, data []byte) error {
			return t.post(ctx, raw, headers, data)
		},
	}, nil
}

// post delivers the envelope. The body is the same WebHookEvent every other transport
// carries, and the subscription's own headers are applied on top — no header of the
// emulator's own invention, since the event name is already in the body and production's
// headers are undocumented.
func (t *webhookTransport) post(ctx context.Context, endpoint string, headers map[string]string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Drain a bounded amount of the response so the connection can be reused; the body
	// itself is of no interest, and a subscriber that streams one must not stall us.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, discardLimit))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("callback answered %s", resp.Status)
	}
	return nil
}

// discardLimit is how much of a subscriber's response body is read before the connection
// is closed rather than reused.
const discardLimit = 4 << 10

// callbackHeaders reads a webhook target's CallbackHeader array into the headers to set.
// A malformed entry is skipped rather than failing the delivery — the headers are extra
// context for the subscriber, not the message.
func callbackHeaders(target map[string]any) map[string]string {
	raw, ok := target["headers"].([]any)
	if !ok {
		return nil
	}

	out := map[string]string{}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if key := mapString(m, "key"); key != "" {
			out[key] = mapString(m, "value")
		}
	}
	return out
}

// localOnlyHosts are the names that can only ever mean this machine.
var localOnlyHosts = map[string]bool{
	"localhost":            true,
	"host.docker.internal": true,
}

// localOnlySuffixes are the domain suffixes reserved for names that do not resolve on
// the public internet.
var localOnlySuffixes = []string{".localhost", ".local", ".internal"}

// isLocalHost reports whether a callbackUrl's host is one the emulator will call without
// being widened: a loopback, private, link-local or unspecified address, a name reserved
// for local use, or a single-label name — which cannot be a public domain and is how a
// service is addressed on a Docker network.
//
// The judgement is on the literal host, with no DNS resolution: a lookup here would be
// slow, would vary with the machine's resolver, and is itself the outbound request this
// check exists to prevent.
func isLocalHost(host string) bool {
	host = strings.ToLower(host)
	if host == "" {
		return false
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified()
	}
	if localOnlyHosts[host] {
		return true
	}
	if !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range localOnlySuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
