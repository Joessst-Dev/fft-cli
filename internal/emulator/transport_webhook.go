package emulator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"syscall"
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
	t := &webhookTransport{allowRemote: allowRemote}

	// A dial-time guard on the *resolved* address, not just the textual host plan()
	// vets. isLocalHost judges u.Hostname() as a string, which a decimal/hex IP
	// encoding or a hostname that resolves to the metadata IP (DNS rebinding) can
	// slip past; Control is handed the concrete ip:port Go is about to connect to, so
	// it sees through both. The metadata endpoint is refused here whatever plan() did.
	dialer := &net.Dialer{
		Control: func(_, address string, _ syscall.RawConn) error {
			return t.allowDial(address)
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext

	t.client = &http.Client{
		// Redirects are not followed: a local endpoint answering 302 with a remote
		// Location would walk the request straight past the checks. Returning the
		// redirect as the response instead makes it a non-2xx, i.e. a logged failure.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     transport,
	}
	return t
}

// allowDial refuses a connection at dial time, judging the concrete address the
// resolver returned rather than the callbackUrl's textual host. The cloud metadata
// endpoint is refused always; any non-local address is refused unless the transport
// was widened. This is the enforcement that cannot be encoded around — isLocalHost
// is the earlier, friendlier refusal with a message, this is the backstop.
func (t *webhookTransport) allowDial(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		// Control is handed a resolved ip:port, so a non-IP here is unexpected — fail
		// closed rather than guess.
		return fmt.Errorf("refusing to dial %q: not an IP address", address)
	}
	if slices.Contains(metadataAddrs, addr.Unmap()) {
		return fmt.Errorf("refusing to dial the cloud metadata endpoint %s", addr)
	}
	if t.allowRemote {
		return nil
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		return nil
	}
	return fmt.Errorf("refusing to dial non-local address %s (--webhook-allow-remote to allow)", addr)
}

// describe says where this transport will call, for the startup notice. The bound is
// the whole of what a user needs to know about webhook delivery, so it is what the
// notice says.
func (t *webhookTransport) describe() string {
	if t.allowRemote {
		return "calling any callbackUrl (--webhook-allow-remote is set)"
	}
	return "calling local callbackUrls (--webhook-allow-remote to widen)"
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

// metadataAddrs are the cloud instance-metadata endpoints (AWS/GCP/Azure IMDS and
// its IPv6 form). They sit in the link-local / ULA ranges [isLocalHost] otherwise
// treats as local, so they are matched out explicitly.
var metadataAddrs = []netip.Addr{
	netip.MustParseAddr("169.254.169.254"),
	netip.MustParseAddr("fd00:ec2::254"),
}

// metadataHosts are the *names* the metadata endpoint answers to. GCP resolves
// metadata.google.internal, metadata.goog, and the bare "metadata" all to
// 169.254.169.254 — and those would sail through localOnlySuffixes (".internal")
// and the single-label rule below, which is the same blind-SSRF as hitting the IP.
// isLocalHost does no DNS, so the names are denied literally.
var metadataHosts = map[string]bool{
	"metadata":                 true,
	"metadata.google.internal": true,
	"metadata.goog":            true,
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

	// The metadata endpoint by name, refused before any acceptance rule can reach it.
	if metadataHosts[host] {
		return false
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		// The cloud instance-metadata endpoints are link-local (or ULA), so the
		// acceptance below would wave them through. A POST to 169.254.169.254 with
		// attacker-chosen headers is precisely the SSRF this guard denies, so it is
		// refused even in local mode, before anything else is considered.
		if slices.Contains(metadataAddrs, addr.Unmap()) {
			return false
		}
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
