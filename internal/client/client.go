// Package client is the fulfillmenttools API as the rest of fft sees it.
//
// It wraps the generated client (which owns the paths, the models and — crucially
// — the per-parameter query encoding) with the five things every command needs and
// none should reimplement:
//
//   - the error envelope decoded as the JSON *array* it actually is, mapped to an
//     exit code and a hint ([APIError], [Check]);
//   - a retry that knows the difference between a request it may repeat and one it
//     may not ([Client.Do]);
//   - the reactive 401: refresh the token and retry, exactly once;
//   - cursor pagination as one generic, because every /search endpoint in the API
//     has the same shape ([Search], [SearchAll]);
//   - optimistic locking as one generic, because `version` travels in the body and
//     every mutation is therefore a read-then-write ([UpdateVersioned]).
//
// # Why the retry is here and not in a RoundTripper
//
// A RoundTripper is handed a request whose body is a one-shot io.ReadCloser. By
// the time a 401 or a 500 comes back the body has been consumed, and replaying it
// is impossible in the general case. A retry at that layer works in the specs,
// where the body is a bytes.Reader, and silently sends an empty POST in
// production. So the retry lives here, where a request is a [Doer] that can simply
// be called again.
package client

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/httplog"
)

// contentTypeJSON is what every fulfillmenttools request body is.
const contentTypeJSON = "application/json"

// Client is one tenant, authenticated.
//
// The generated client underneath is reachable through [Client.API] for the calls
// that need no retry — `fft ping` against /api/status, say. Everything else goes
// through [Client.Do], which is what turns a generated call into one that retries
// safely, refreshes on a 401, and fails with a message the user can act on.
type Client struct {
	api    *api.ClientWithResponses
	source auth.TokenSource
	retry  Retry

	// baseURL and hc are the raw request path ([Client.DoRaw]): the generated client
	// covers five tags, and the other 451 operations are reached by building the
	// request from spec metadata and sending it with the same signed, logged,
	// TLS-floored HTTP client the generated one uses.
	baseURL string
	hc      *http.Client
}

// Option configures the client.
type Option func(*options)

type options struct {
	source auth.TokenSource
	hc     *http.Client
	debug  io.Writer
	retry  Retry
}

// WithTokenSource authenticates every request with src.
//
// Without it the client is unauthenticated, which is exactly what `fft ping`
// wants: /api/status is the one endpoint that answers without a token, so it can
// prove connectivity even when the credentials are the thing that is broken.
func WithTokenSource(src auth.TokenSource) Option {
	return func(o *options) { o.source = src }
}

// WithHTTPClient replaces the underlying HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(o *options) { o.hc = hc }
}

// WithDebug logs every request and response to w, redacted. This is --debug.
func WithDebug(w io.Writer) Option {
	return func(o *options) { o.debug = w }
}

// WithRetry replaces the retry policy. Fields left zero keep their default.
func WithRetry(r Retry) Option {
	return func(o *options) { o.retry = r }
}

// New returns the client for the tenant at baseURL.
func New(baseURL string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("the project has no base URL")
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	hc := o.hc
	if hc == nil {
		hc = &http.Client{Transport: transport()}
	}

	// The layering matters. The logger goes underneath the bearer transport so that
	// it sees the Authorization header it must redact; the bearer transport goes on
	// top so that a retried request is signed with the token that was current when
	// it was retried, not when it was first built.
	base := hc.Transport
	if base == nil {
		base = transport()
	}
	if o.debug != nil {
		base = httplog.New(o.debug, base)
	}
	if o.source != nil {
		base = &auth.Transport{Source: o.source, Base: base}
	}

	signed := &http.Client{
		Transport:     base,
		Timeout:       hc.Timeout,
		CheckRedirect: hc.CheckRedirect,
		Jar:           hc.Jar,
	}

	c, err := api.NewClientWithResponses(baseURL, api.WithHTTPClient(signed))
	if err != nil {
		return nil, fmt.Errorf("build the API client for %s: %w", baseURL, err)
	}

	return &Client{
		api:     c,
		source:  o.source,
		retry:   o.retry.withDefaults(),
		baseURL: baseURL,
		hc:      signed,
	}, nil
}

// API is the generated client, for a call that does not go through [Client.Do].
func (c *Client) API() *api.ClientWithResponses { return c.api }

// transport is the default transport for tenant traffic: TLS 1.2 as the floor,
// because the bearer token rides on every request.
func transport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	t.ResponseHeaderTimeout = 60 * time.Second
	return t
}

// APIError is a non-2xx answer from fulfillmenttools.
//
// The error envelope is a JSON *array* of ErrorInner — [{"summary":"…"}] — and not
// an object. Decoding it into a struct succeeds and produces {} for every error,
// which is how an API turns "no facility matching request X was found" into
// silence.
type APIError struct {
	// Status is the HTTP status code.
	Status int
	// Errors is the decoded envelope. It is empty when the body was not one — a
	// proxy's HTML 502, say.
	Errors []api.ErrorInner
	// Body is the raw response, kept only when it could not be decoded, so that
	// the user is told *something* rather than "HTTP 502".
	Body string
}

func (e *APIError) Error() string {
	if sent, current, ok := e.Conflict(); ok {
		return fmt.Sprintf("version conflict: you sent v%d, current is v%d", sent, current)
	}

	if len(e.Errors) == 0 {
		if e.Body == "" {
			return fmt.Sprintf("the API returned HTTP %d", e.Status)
		}
		return fmt.Sprintf("the API returned HTTP %d: %s", e.Status, e.Body)
	}

	summaries := make([]string, 0, len(e.Errors))
	for _, inner := range e.Errors {
		summaries = append(summaries, inner.Summary)
	}
	return fmt.Sprintf("the API returned HTTP %d: %s", e.Status, strings.Join(summaries, "; "))
}

// Conflict reports the two versions a 409 carries: the one the request sent and
// the one the server holds. ok is false for anything else — including a 409 whose
// envelope, for once, did not carry them.
//
// This is the gift the array envelope brings: the server tells us exactly how
// stale we were, so the user is told too, instead of "409 Conflict".
func (e *APIError) Conflict() (sent, current int64, ok bool) {
	if e.Status != http.StatusConflict {
		return 0, 0, false
	}

	for _, inner := range e.Errors {
		if inner.RequestVersion != nil && inner.Version != nil {
			return *inner.RequestVersion, *inner.Version, true
		}
	}
	return 0, 0, false
}

// ExitCode implements the interface exitcode.FromError looks for, so a script can
// tell "you are not permitted" from "it does not exist" without parsing text.
func (e *APIError) ExitCode() int {
	switch {
	case e.Status == http.StatusUnauthorized:
		return exitcode.Auth
	case e.Status == http.StatusForbidden:
		return exitcode.Forbidden
	case e.Status == http.StatusNotFound:
		return exitcode.NotFound
	case e.Status == http.StatusConflict:
		return exitcode.Conflict
	case e.Status >= http.StatusInternalServerError:
		return exitcode.Unavailable
	default:
		return exitcode.General
	}
}

// Hint names the command that would fix the failure, where there is one.
func (e *APIError) Hint() string {
	if _, current, ok := e.Conflict(); ok {
		return fmt.Sprintf("Re-run the command to read the current version, or pass --if-version %d.", current)
	}

	switch e.Status {
	case http.StatusUnauthorized:
		return "Run 'fft auth refresh', or 'fft project add <name> --force' to sign in again."
	case http.StatusForbidden:
		return "Run 'fft auth whoami' to see which permissions this account actually has."
	case http.StatusTooManyRequests:
		return "The API is rate-limiting this tenant. Try again in a moment."
	default:
		return ""
	}
}

// RequestError reports a request that never got an answer.
//
// http.Client wraps every transport failure in a *url.Error, which prints as
// `Get "https://…/api/…": <cause>`. When the cause is fft's own — a token it could
// not mint — that prefix buries the one sentence the user needs ("sign in again")
// behind a URL they did not type. So an error that already knows its exit code is
// returned as it is, with its hint intact; a genuine network failure keeps the
// URL, because there the URL is the point.
func RequestError(op string, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		var coded interface{ ExitCode() int }
		if errors.As(urlErr.Err, &coded) {
			if cause, ok := coded.(error); ok {
				return cause
			}
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}

// maxBody caps how much of an error body is quoted back at the user. A proxy that
// answers with a megabyte of HTML should not fill the terminal.
const maxBody = 512

// Check turns a non-2xx response into an *APIError, and returns nil for a 2xx.
func Check(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}

	e := &APIError{Status: status}
	if err := json.Unmarshal(body, &e.Errors); err != nil || len(e.Errors) == 0 {
		e.Errors = nil
		e.Body = truncate(strings.TrimSpace(string(body)), maxBody)
	}
	return e
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
