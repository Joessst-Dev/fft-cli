// Package httplog logs HTTP traffic for --debug, with the secrets taken out.
//
// It is a leaf package on purpose. Both the traffic to Google (whose request URL
// carries the Firebase API key and whose request body carries the password) and
// the traffic to fulfillmenttools (whose every request carries a bearer token)
// have to be logged by the same rules, and the two packages that own those
// clients — auth and client — must not import each other. The redaction lives
// here so that there is exactly one implementation of it.
//
// # What is removed
//
// The Authorization header, the ?key= query parameter, and any password, token or
// refresh token appearing as a JSON field or a form field. A --debug dump is the
// single most likely thing a user pastes into a bug report, so the bar is not
// "unlikely to leak" but "cannot".
package httplog

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Redacted is what a secret is replaced with. It is deliberately conspicuous: a
// reader who sees it knows something was removed, rather than wondering whether
// the value really was empty.
const Redacted = "[REDACTED]"

// maxBody caps how much of a body is logged. Enough to see the shape of a
// request; not enough for a 2,000-facility page to bury the terminal.
const maxBody = 4 << 10

var (
	// keyParam matches the ?key= / &key= carrying the Firebase Web API key. Every
	// error the standard library produces from an HTTP request quotes the whole
	// URL, so without this the key lands in the user's terminal — and from there
	// in the bug report they paste it into.
	keyParam = regexp.MustCompile(`(?i)([?&]key=)[^&\s"']*`)

	// jsonSecret matches a sensitive JSON string field. Google's sign-in request
	// carries the password and its answer carries both tokens.
	jsonSecret = regexp.MustCompile(`(?i)"(password|idToken|refreshToken|id_token|refresh_token|access_token)"(\s*:\s*)"[^"]*"`)

	// formSecret matches the same secrets in an x-www-form-urlencoded body, which
	// is the shape the token refresh is sent in.
	formSecret = regexp.MustCompile(`(?i)\b(password|refresh_token|id_token|access_token)=[^&\s]*`)
)

// sensitiveHeaders are logged as their name only. A bearer token is not more
// useful to a reader than the fact that one was sent.
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
}

// Redact removes from s the API key, any password or token it carries, and any of
// the extra secrets given that appear in it verbatim.
//
// Very short extras are left alone: replacing every occurrence of a three-letter
// password would mangle the message into uselessness, and the message is the only
// thing the user has to go on.
func Redact(s string, extra ...string) string {
	for _, secret := range extra {
		if len(secret) < 8 {
			continue
		}
		s = strings.ReplaceAll(s, secret, Redacted)
	}

	s = keyParam.ReplaceAllString(s, "${1}"+Redacted)
	s = jsonSecret.ReplaceAllString(s, `"${1}"${2}"`+Redacted+`"`)
	return formSecret.ReplaceAllString(s, "${1}="+Redacted)
}

// Transport logs every request it carries, and the response to it, to w.
//
// It is a RoundTripper rather than a wrapper around the call sites because that is
// the only layer that sees what was *actually* sent: the headers another
// RoundTripper added, the body the generated client encoded. It changes nothing
// about the request.
type Transport struct {
	w    io.Writer
	base http.RoundTripper
	now  func() time.Time

	// mu keeps two concurrent requests from interleaving their lines. A CLI mostly
	// issues one request at a time, but a dump that cannot be read is not a dump.
	mu sync.Mutex
}

var _ http.RoundTripper = (*Transport)(nil)

// New returns a Transport logging to w. A nil base means http.DefaultTransport.
func New(w io.Writer, base http.RoundTripper) *Transport {
	return &Transport{w: w, base: base, now: time.Now}
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.w == nil {
		return t.roundTrip(req)
	}

	t.logRequest(req)

	start := t.now()
	res, err := t.roundTrip(req)
	elapsed := t.now().Sub(start).Round(time.Millisecond)

	if err != nil {
		t.printf("< %s (after %s)\n\n", Redact(err.Error()), elapsed)
		return nil, err
	}

	t.logResponse(res, elapsed)
	return res, nil
}

func (t *Transport) roundTrip(req *http.Request) (*http.Response, error) {
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func (t *Transport) logRequest(req *http.Request) {
	var b strings.Builder
	fmt.Fprintf(&b, "> %s %s\n", req.Method, Redact(req.URL.String()))
	writeHeaders(&b, ">", req.Header)

	// req.Body is a one-shot reader that belongs to the transport underneath; only
	// GetBody may be read, and only the standard library sets it — for a body it
	// knows how to replay.
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			defer body.Close()
			if snippet, ok := read(body); ok {
				fmt.Fprintf(&b, "> %s\n", snippet)
			}
		}
	}

	t.print(b.String())
}

func (t *Transport) logResponse(res *http.Response, elapsed time.Duration) {
	var b strings.Builder
	fmt.Fprintf(&b, "< %s (in %s)\n", res.Status, elapsed)
	writeHeaders(&b, "<", res.Header)

	// The body belongs to the caller. Only the part that is logged is buffered; the
	// rest still streams, so a --debug run of a large page does not hold it all in
	// memory.
	head, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err == nil {
		if snippet, ok := snippet(head); ok {
			fmt.Fprintf(&b, "< %s\n", snippet)
		}
		res.Body = &joinedBody{head: bytes.NewReader(head), rest: res.Body}
	}

	b.WriteString("\n")
	t.print(b.String())
}

// joinedBody re-attaches the bytes the logger read to the ones still on the wire.
// Closing it closes the response body, which is what the caller's defer expects.
type joinedBody struct {
	head *bytes.Reader
	rest io.ReadCloser
}

func (b *joinedBody) Read(p []byte) (int, error) {
	if b.head.Len() > 0 {
		return b.head.Read(p)
	}
	return b.rest.Read(p)
}

func (b *joinedBody) Close() error { return b.rest.Close() }

func writeHeaders(b *strings.Builder, prefix string, h http.Header) {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	// Sorted, so that two runs of the same command produce a diffable dump.
	sort.Strings(names)

	for _, name := range names {
		value := strings.Join(h.Values(name), ", ")
		if sensitiveHeaders[strings.ToLower(name)] {
			value = Redacted
		}
		fmt.Fprintf(b, "%s %s: %s\n", prefix, name, Redact(value))
	}
}

// read renders a body for the log, reporting whether there was one at all.
func read(body io.Reader) (string, bool) {
	raw, err := io.ReadAll(io.LimitReader(body, maxBody+1))
	if err != nil {
		return "", false
	}
	return snippet(raw)
}

func snippet(raw []byte) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}

	truncated := len(raw) > maxBody
	if truncated {
		raw = raw[:maxBody]
	}

	// A body is quoted on one line: a request is one entry in the dump, and a
	// pretty-printed JSON page would drown every other entry.
	s := strings.Join(strings.Fields(string(raw)), " ")
	if truncated {
		s += " … (truncated)"
	}
	return Redact(s), true
}

func (t *Transport) print(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// A failed write to the debug stream is not worth failing the command over: the
	// user asked for a trace, not for a guarantee.
	_, _ = io.WriteString(t.w, s)
}

func (t *Transport) printf(format string, args ...any) {
	t.print(fmt.Sprintf(format, args...))
}
