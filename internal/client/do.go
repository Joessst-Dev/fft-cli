package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
)

// maxResponse caps how much of a response is read into memory. A search page of
// 250 facilities is a few hundred kilobytes; anything vastly larger is a captive
// portal or a proxy error page, and reading it unbounded is a denial of service we
// would be performing on ourselves.
const maxResponse = 64 << 20

// Doer issues one attempt of a request.
//
// It must build a *fresh* request every time it is called, because [Client.Do]
// calls it again to retry and an http.Request body is a one-shot reader. Every
// generated api.ClientInterface method satisfies this: each call marshals the body
// anew. A Doer that closes over an io.Reader does not, and would retry by sending
// an empty body.
type Doer func(ctx context.Context) (*http.Response, error)

// Result is a 2xx answer: everything a caller could want from it, with the body
// already read and the connection already returned to the pool.
type Result struct {
	Status int
	Header http.Header
	Body   []byte
}

// Retry is the retry policy. A zero Retry means the defaults.
//
// # The idempotency rule
//
// A 429 is retried for every method: the request provably did not happen. A 5xx or
// a dropped connection is retried only for GET, PUT and DELETE — a POST that
// answered 500 may still have created the facility, and re-sending it would create
// a second one. There is no way to tell from the outside, so fft does not guess:
// it surfaces the error and lets the user look.
type Retry struct {
	// MaxAttempts is how many requests may be sent in total, the first included.
	MaxAttempts int

	// Base is the first backoff interval; it doubles from there.
	Base time.Duration

	// Max caps one backoff interval.
	Max time.Duration

	// MaxWait is the longest Retry-After fft will honour. A rate limit measured in
	// minutes is not something a CLI should sit and wait out — it is something the
	// user should be told about.
	MaxWait time.Duration

	// Sleep waits, or returns the context's error if the wait is cancelled. It is a
	// seam: nil means a real sleep, and specs replace it so that a spec exercising
	// a Retry-After of one second does not take one second.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Retry defaults. Three attempts because a CLI that hangs on a struggling API is
// worse than one that fails and lets the user re-run it.
const (
	defaultMaxAttempts = 3
	defaultBase        = 200 * time.Millisecond
	defaultMax         = 5 * time.Second
	defaultMaxWait     = 30 * time.Second
)

func (r Retry) withDefaults() Retry {
	if r.MaxAttempts <= 0 {
		r.MaxAttempts = defaultMaxAttempts
	}
	if r.Base <= 0 {
		r.Base = defaultBase
	}
	if r.Max <= 0 {
		r.Max = defaultMax
	}
	if r.MaxWait <= 0 {
		r.MaxWait = defaultMaxWait
	}
	if r.Sleep == nil {
		r.Sleep = sleep
	}
	return r
}

// Do issues the request, retrying it where that is safe, and turns the answer into
// a *Result or an error a user can act on.
//
// op names the operation in errors ("list the facilities"), so that a failure
// reads as a sentence rather than as a URL.
func (c *Client) Do(ctx context.Context, op string, do Doer) (*Result, error) {
	if do == nil {
		return nil, fmt.Errorf("%s: no request to send", op)
	}
	policy := c.retry.withDefaults()

	// The reactive 401 gets exactly one chance. The bool — not a counter, not a
	// condition on the response — is what makes a refresh loop structurally
	// impossible: a second 401 cannot reach the refresh branch, whatever the server
	// does.
	refreshed := false
	sent := 0

	for {
		res, err := do(ctx)
		sent++

		if err != nil {
			// A cancelled context is the user pressing ^C or --timeout expiring. It is
			// not a flaky network, and retrying it would only delay the exit.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if sent < policy.MaxAttempts && retryable(err) && idempotent(methodOf(err)) {
				if err := policy.wait(ctx, policy.backoff(sent)); err != nil {
					return nil, err
				}
				continue
			}
			return nil, RequestError(op, err)
		}

		result, err := read(res)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}

		switch {
		case result.Status == http.StatusUnauthorized && !refreshed && c.renewer() != nil:
			refreshed = true
			if err := c.refresh(ctx); err != nil {
				return nil, err
			}
			continue

		case result.Status == http.StatusTooManyRequests && sent < policy.MaxAttempts:
			// A 429 is safe to retry whatever the method: the request did not happen.
			wait, ok := policy.throttle(result.Header, sent)
			if !ok {
				break
			}
			if err := policy.wait(ctx, wait); err != nil {
				return nil, err
			}
			continue

		case result.Status >= http.StatusInternalServerError &&
			sent < policy.MaxAttempts &&
			idempotent(res.Request.Method):
			if err := policy.wait(ctx, policy.backoff(sent)); err != nil {
				return nil, err
			}
			continue
		}

		if err := Check(result.Status, result.Body); err != nil {
			return nil, err
		}
		return result, nil
	}
}

// Fetch issues the request and decodes its JSON answer into T.
func Fetch[T any](ctx context.Context, c *Client, op string, do Doer) (T, error) {
	var out T
	if c == nil {
		return out, fmt.Errorf("%s: there is no API client", op)
	}

	res, err := c.Do(ctx, op, do)
	if err != nil {
		return out, err
	}
	if len(res.Body) == 0 {
		return out, fmt.Errorf("%s: the API answered with an empty body", op)
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return out, fmt.Errorf("%s: decode the API's answer: %w", op, err)
	}
	return out, nil
}

// renewer is the token source, if it is one that can be forced to mint a fresh
// token. A static FFT_ID_TOKEN is not: there is nothing behind it to renew from,
// so a 401 on it is final and retrying would only send the same dead token twice.
func (c *Client) renewer() auth.Renewer {
	r, ok := c.source.(auth.Renewer)
	if !ok {
		return nil
	}
	return r
}

// refresh forces a new token after a 401. The next attempt picks it up through the
// bearer transport, which asks the source for a token on every request.
func (c *Client) refresh(ctx context.Context) error {
	if _, err := c.renewer().Renew(ctx); err != nil {
		return err
	}
	return nil
}

// read consumes and closes the response body, which is what returns the connection
// to the pool. Everything after this point works on bytes.
func read(res *http.Response) (*Result, error) {
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return nil, fmt.Errorf("read the API's answer: %w", err)
	}
	return &Result{Status: res.StatusCode, Header: res.Header, Body: body}, nil
}

// idempotent reports whether a request of this method may be sent twice.
//
// POST and PATCH may not. POST /api/facilities and POST /api/stocks are creates:
// a 500 does not mean the facility was not created, it means we were not told.
// An empty method is not idempotent either — see [methodOf], which fails closed.
func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// methodOf recovers the request's method from a transport failure, where there is
// no response to read it off.
//
// http.Client builds every such failure as a *url.Error whose Op is the method in
// title case ("Get", "Post"). If that ever stops being true this returns "", which
// [idempotent] treats as not-retryable — the failure mode is a request that is not
// repeated, never a POST that is.
func methodOf(err error) string {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return ""
	}
	return strings.ToUpper(urlErr.Op)
}

// retryable reports whether a transport failure is worth another try: the
// connection died, rather than the request being wrong or the credentials being
// refused.
func retryable(err error) bool {
	switch {
	case errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF):
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// backoff is exponential with full jitter: a random point in [0, base·2^n), capped.
// The jitter is the part that matters — without it, every client that failed
// together retries together, and the API's bad second becomes a bad minute.
func (r Retry) backoff(attempt int) time.Duration {
	window := r.Base << min(attempt-1, 16)
	if window > r.Max || window <= 0 {
		window = r.Max
	}
	return rand.N(window)
}

// throttle reads Retry-After, falling back to the backoff when the header is
// absent or unparseable. ok is false when the API asks for longer than [Retry.MaxWait]:
// a CLI that silently sits for five minutes looks broken, so that surfaces as the
// 429 it is.
func (r Retry) throttle(h http.Header, attempt int) (time.Duration, bool) {
	value := strings.TrimSpace(h.Get("Retry-After"))
	if value == "" {
		return r.backoff(attempt), true
	}

	var wait time.Duration
	switch seconds, err := strconv.Atoi(value); {
	case err == nil:
		wait = time.Duration(seconds) * time.Second
	default:
		at, err := http.ParseTime(value)
		if err != nil {
			return r.backoff(attempt), true
		}
		wait = time.Until(at)
	}

	if wait < 0 {
		wait = 0
	}
	if wait > r.MaxWait {
		return 0, false
	}
	return wait, true
}

// wait sleeps, unless the context is cancelled first.
func (r Retry) wait(ctx context.Context, d time.Duration) error {
	if err := r.Sleep(ctx, d); err != nil {
		return err
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
