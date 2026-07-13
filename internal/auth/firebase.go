package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/httplog"
)

// Google's identity endpoints. These are the only two hosts the Firebase Web API
// key may ever be sent to, and the only two [Client] will talk to.
const (
	signInEndpoint  = "https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword"
	refreshEndpoint = "https://securetoken.googleapis.com/v1/token"
)

// maxResponse caps how much of a response body is read. Google answers in a few
// hundred bytes; anything vastly larger is a captive portal or a proxy error
// page, and reading it into memory unbounded is a denial of service we would be
// performing on ourselves.
const maxResponse = 1 << 20

// Client talks to Google Identity Platform on behalf of one Firebase project.
//
// It owns its HTTP client rather than accepting one, and that client's transport
// refuses any host but Google's two identity endpoints. The point is not to
// distrust the caller: it is that the API key rides on every request as a query
// parameter, so the only way to guarantee it never reaches fulfillmenttools is to
// make a request there impossible to send.
type Client struct {
	apiKey  string
	signIn  string
	refresh string
	now     func() time.Time
	debug   io.Writer
	hc      *http.Client
}

// Option configures a [Client].
type Option func(*Client)

// WithClock replaces the clock a token's expiry is computed against. Specs use it
// to make "this token has four minutes left" a fact rather than a race.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// WithDebug logs the identity traffic to w, which is what --debug does. The
// request URL carries the API key and the request body carries the password, so
// the dump is redacted — see [httplog.Redact].
func WithDebug(w io.Writer) Option {
	return func(c *Client) { c.debug = w }
}

// withEndpoints points the client at fake identity servers.
//
// It is unexported on purpose, and so is the allowlist it widens: no code outside
// this package can redirect the Firebase API key at a host of its choosing. The
// specs are in this package precisely so that this stays true of everything else.
func withEndpoints(signIn, refresh string) Option {
	return func(c *Client) {
		c.signIn, c.refresh = signIn, refresh
	}
}

// NewClient returns a Client for the Firebase project identified by apiKey.
func NewClient(apiKey string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("the Firebase Web API key is empty")
	}

	c := &Client{
		apiKey:  apiKey,
		signIn:  signInEndpoint,
		refresh: refreshEndpoint,
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}

	hosts, err := hostsOf(c.signIn, c.refresh)
	if err != nil {
		return nil, err
	}

	// The logger sits *under* the allowlist, so that it records what was actually
	// sent rather than what was about to be refused.
	base := baseTransport()
	if c.debug != nil {
		base = httplog.New(c.debug, base)
	}
	c.hc = &http.Client{Transport: &allowlistTransport{allowed: hosts, base: base}}

	return c, nil
}

// baseTransport is a fresh transport rather than http.DefaultTransport, so that
// nothing else in the process can reconfigure the connection the API key travels
// over. TLS 1.2 is the floor: 1.0 and 1.1 are broken and Google does not offer
// them anyway, so pinning the minimum costs nothing and closes a downgrade.
func baseTransport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return t
}

func hostsOf(rawURLs ...string) (map[string]struct{}, error) {
	hosts := make(map[string]struct{}, len(rawURLs))
	for _, raw := range rawURLs {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse the identity endpoint: %w", err)
		}
		hosts[u.Hostname()] = struct{}{}
	}
	return hosts, nil
}

// allowlistTransport refuses to send a request to any host but the ones it was
// built with. It is the last line of defence for the API key, and it fails closed.
type allowlistTransport struct {
	allowed map[string]struct{}
	base    http.RoundTripper
}

func (t *allowlistTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if _, ok := t.allowed[host]; !ok {
		// The URL itself is not quoted: it carries the key we are refusing to leak.
		return nil, fmt.Errorf("auth: refusing to send the Firebase API key to %q", host)
	}
	return t.base.RoundTrip(req)
}

// signInResponse is Google's identitytoolkit answer. camelCase — see
// [refreshResponse], which is not.
type signInResponse struct {
	IDToken      string `json:"idToken"`
	RefreshToken string `json:"refreshToken"`
	// ExpiresIn is a count of seconds delivered as a JSON *string* ("3600").
	// Declaring it int fails to unmarshal, and the zero Token that a tolerant
	// decoder would leave behind is worse.
	ExpiresIn string `json:"expiresIn"`
	Email     string `json:"email"`
	LocalID   string `json:"localId"`
}

// refreshResponse is Google's securetoken answer. snake_case, and a different
// shape from [signInResponse]: one struct for both decodes without error into an
// empty token.
type refreshResponse struct {
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	// ExpiresIn is a JSON string here too.
	ExpiresIn string `json:"expires_in"`
	UserID    string `json:"user_id"`
}

// SignIn exchanges an email and password for a token.
func (c *Client) SignIn(ctx context.Context, email, password string) (Token, error) {
	body, err := json.Marshal(map[string]any{
		"email":             email,
		"password":          password,
		"returnSecureToken": true,
	})
	if err != nil {
		return Token{}, fmt.Errorf("encode the sign-in request: %w", err)
	}

	req, err := c.newRequest(ctx, c.signIn, "application/json", body)
	if err != nil {
		return Token{}, err
	}

	var res signInResponse
	if err := c.do(req, &res, password); err != nil {
		return Token{}, fmt.Errorf("sign in as %s: %w", email, err)
	}

	tok, err := c.token(res.IDToken, res.RefreshToken, res.ExpiresIn, res.Email)
	if err != nil {
		return Token{}, fmt.Errorf("sign in as %s: %w", email, err)
	}
	if tok.Email == "" {
		tok.Email = email
	}
	return tok, nil
}

// Refresh renews an id token from a refresh token, without the password.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	req, err := c.newRequest(ctx, c.refresh, "application/x-www-form-urlencoded", []byte(form.Encode()))
	if err != nil {
		return Token{}, err
	}

	var res refreshResponse
	if err := c.do(req, &res, refreshToken); err != nil {
		return Token{}, fmt.Errorf("refresh the id token: %w", err)
	}

	// The refresh response carries no email; the caller keeps the one it already
	// knows.
	tok, err := c.token(res.IDToken, res.RefreshToken, res.ExpiresIn, "")
	if err != nil {
		return Token{}, fmt.Errorf("refresh the id token: %w", err)
	}
	return tok, nil
}

func (c *Client) newRequest(ctx context.Context, endpoint, contentType string, body []byte) (*http.Request, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse the identity endpoint: %w", err)
	}
	u.RawQuery = url.Values{"key": {c.apiKey}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		// The URL holds the key, so the error the stdlib built from it cannot be
		// shown as it is.
		return nil, redact(fmt.Errorf("build the identity request: %w", err), c.apiKey)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// do sends the request and decodes a successful response into out. Every error it
// returns is redacted: the request URL carries the API key and the request body
// carries the password.
func (c *Client) do(req *http.Request, out any, extraSecrets ...string) error {
	secrets := append([]string{c.apiKey}, extraSecrets...)

	res, err := c.hc.Do(req)
	if err != nil {
		return redact(err, secrets...)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return redact(fmt.Errorf("read the identity response: %w", err), secrets...)
	}

	if res.StatusCode != http.StatusOK {
		return parseError(res.StatusCode, body)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return redact(fmt.Errorf("decode the identity response: %w", err), secrets...)
	}
	return nil
}

func (c *Client) token(id, refresh, expiresIn, email string) (Token, error) {
	if id == "" || refresh == "" {
		return Token{}, fmt.Errorf("google returned no token")
	}

	seconds, err := strconv.Atoi(strings.TrimSpace(expiresIn))
	if err != nil || seconds <= 0 {
		return Token{}, fmt.Errorf("google returned the lifetime %q, which is not a number of seconds", expiresIn)
	}

	return Token{
		ID:        id,
		Refresh:   refresh,
		ExpiresAt: c.now().Add(time.Duration(seconds) * time.Second),
		Email:     email,
	}, nil
}

// Error is Google Identity Platform refusing a credential.
//
// Code is the machine-readable reason (TOKEN_EXPIRED, INVALID_LOGIN_CREDENTIALS,
// …) that the caller branches on; the message is the sentence the user reads.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	explanation := explain(e.Code)
	if e.Code == "" {
		return fmt.Sprintf("%s (HTTP %d)", explanation, e.Status)
	}
	return fmt.Sprintf("%s (%s)", explanation, e.Code)
}

// ExitCode implements the interface exitcode.FromError looks for. A refusal is an
// authentication failure; a 5xx is Google having a bad day, which is a different
// thing for a script to react to.
func (e *Error) ExitCode() int {
	if e.Status >= http.StatusInternalServerError {
		return exitcode.Unavailable
	}
	return exitcode.Auth
}

// Refused reports whether Google rejected the credential itself, as opposed to
// the network failing on the way there or Google failing behind it.
//
// Only a refusal justifies burning the password on a fresh sign-in: a timeout
// must surface as a timeout, not as "your refresh token is dead".
func Refused(err error) bool {
	var gerr *Error
	return errors.As(err, &gerr) && gerr.Status < http.StatusInternalServerError
}

// explain turns Google's code into something a user can act on. Unknown codes are
// passed through: a code we have never seen is still more useful than "an error
// occurred".
func explain(code string) string {
	switch code {
	case "EMAIL_NOT_FOUND", "INVALID_PASSWORD", "INVALID_LOGIN_CREDENTIALS", "INVALID_EMAIL":
		return "the email address or password is wrong"
	case "USER_DISABLED":
		return "the account is disabled"
	case "TOKEN_EXPIRED", "INVALID_REFRESH_TOKEN", "USER_NOT_FOUND":
		return "the stored refresh token is no longer valid"
	case "TOO_MANY_ATTEMPTS_TRY_LATER":
		return "too many attempts; Google is rate-limiting this account"
	case "API_KEY_INVALID", "API key not valid. Please pass a valid API key.":
		return "the Firebase Web API key is not valid for this project"
	case "":
		return "google identity rejected the request"
	default:
		return "google identity rejected the request: " + code
	}
}

// googleErrorBody is Google's error envelope: {"error":{"code":400,"message":"TOKEN_EXPIRED"}}.
type googleErrorBody struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func parseError(status int, body []byte) error {
	var envelope googleErrorBody
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error.Message == "" {
		// Not Google's envelope at all — a proxy's HTML error page, say. The status
		// is all we can honestly report.
		return &Error{Status: status}
	}

	msg := envelope.Error.Message
	// Some messages carry detail after the code ("WEAK_PASSWORD : Password should
	// be at least 6 characters"); the code is the part callers branch on.
	code, _, _ := strings.Cut(msg, " : ")

	return &Error{Status: status, Code: strings.TrimSpace(code), Message: msg}
}
