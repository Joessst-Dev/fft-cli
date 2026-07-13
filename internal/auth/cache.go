package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// ErrReauthRequired means fft has run out of ways to authenticate: the refresh
// token is dead and no stored password could replace it. The only cure is the
// user typing their password again.
var ErrReauthRequired = errors.New("re-authentication required")

// ReauthError carries [ErrReauthRequired] together with the project it happened
// to, so the hint can name the exact command that fixes it.
type ReauthError struct {
	Project string
	Err     error
}

func (e *ReauthError) Error() string {
	return fmt.Sprintf("cannot authenticate as project %q: %v", e.Project, e.Err)
}

// Unwrap returns both the sentinel and the cause, so that errors.Is finds
// ErrReauthRequired and errors.As still finds the [Error] underneath it.
func (e *ReauthError) Unwrap() []error { return []error{ErrReauthRequired, e.Err} }

// ExitCode implements the interface exitcode.FromError looks for.
func (e *ReauthError) ExitCode() int { return exitcode.Auth }

// Hint tells the user the one thing they can do about it.
func (e *ReauthError) Hint() string {
	return fmt.Sprintf("Run 'fft project add %s --force' to sign in again.", e.Project)
}

// mintFunc mints a token to replace prev. prev is the token being retired — it
// may be expired, and its refresh token may still work; it is the zero Token when
// there is nothing cached at all.
type mintFunc func(ctx context.Context, prev Token) (Token, error)

// cachingTokenSource serves a cached token while it is fresh and mints a new one
// when it is not, persisting every mint so that the next fft process does not
// have to sign in again.
//
// The mutex is held across the mint, which serialises concurrent callers: the
// first mints, the rest wait and then find a fresh token. Ten commands in an
// errgroup therefore cost one sign-in, not ten. A caller waiting on the mutex
// cannot be cancelled mid-wait — acceptable in a CLI, where the wait is one HTTP
// round trip.
type cachingTokenSource struct {
	mint    mintFunc
	store   secrets.Store
	project string
	now     func() time.Time

	mu     sync.Mutex
	token  Token
	loaded bool
}

func newCachingTokenSource(mint mintFunc, store secrets.Store, project string, now func() time.Time) *cachingTokenSource {
	if now == nil {
		now = time.Now
	}
	return &cachingTokenSource{mint: mint, store: store, project: project, now: now}
}

// Token implements [TokenSource]. It refreshes proactively: a token with less
// than [Leeway] left is replaced now rather than expiring mid-request.
func (c *cachingTokenSource) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.loaded {
		c.token = loadToken(c.store, c.project)
		c.loaded = true
	}
	if c.token.Fresh(c.now(), Leeway) {
		return c.token.ID, nil
	}

	tok, err := c.renew(ctx)
	if err != nil {
		return "", err
	}
	return tok.ID, nil
}

// Renew implements [Renewer]: it mints a new token even though the cached one is
// still good. `fft auth refresh` is how a user proves the refresh path works
// against their tenant, so it must not be short-circuited by the cache.
func (c *cachingTokenSource) Renew(ctx context.Context) (Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.loaded {
		c.token = loadToken(c.store, c.project)
		c.loaded = true
	}
	return c.renew(ctx)
}

// renew mints and caches a token. The caller holds the mutex.
func (c *cachingTokenSource) renew(ctx context.Context) (Token, error) {
	tok, err := c.mint(ctx, c.token)
	if err != nil {
		return Token{}, err
	}

	c.token = tok
	if err := saveToken(c.store, c.project, tok); err != nil {
		return Token{}, err
	}
	return tok, nil
}

// FirebaseTokenSource authenticates one project against Google Identity Platform,
// caching the id token in memory and in the credential store.
//
// It refreshes with the stored refresh token when it can, and falls back to a
// full password sign-in when that token is dead — which it eventually always is.
type FirebaseTokenSource struct {
	*cachingTokenSource

	client  *Client
	project string
	email   string
	store   secrets.Store
}

var _ TokenSource = (*FirebaseTokenSource)(nil)
var _ Renewer = (*FirebaseTokenSource)(nil)

// NewFirebaseTokenSource returns the TokenSource for project p. The password is
// read from store at the moment it is needed, rather than held in this struct for
// the lifetime of the process.
func NewFirebaseTokenSource(client *Client, p config.Project, store secrets.Store, now func() time.Time) *FirebaseTokenSource {
	s := &FirebaseTokenSource{
		client:  client,
		project: p.Name,
		email:   p.Email,
		store:   store,
	}
	s.cachingTokenSource = newCachingTokenSource(s.mint, store, p.Name, now)
	return s
}

// mint renews prev's token, and signs in from scratch if that is not possible.
//
// The distinction that matters here is between Google *refusing* the refresh
// token and Google being *unreachable*. Only a refusal means the token is dead;
// a timeout means the network is down, and answering that by sending the user's
// password is both pointless and a worse thing to do with a password.
func (s *FirebaseTokenSource) mint(ctx context.Context, prev Token) (Token, error) {
	var refreshErr error

	if prev.Refresh != "" {
		tok, err := s.client.Refresh(ctx, prev.Refresh)
		switch {
		case err == nil:
			if tok.Email == "" {
				tok.Email = s.email
			}
			return tok, nil
		case !Refused(err):
			return Token{}, err
		}
		refreshErr = err
	}

	password, err := s.password()
	if err != nil {
		return Token{}, err
	}
	if password == "" {
		return Token{}, &ReauthError{
			Project: s.project,
			Err:     errors.Join(refreshErr, errors.New("no password is stored for this project")),
		}
	}

	tok, err := s.client.SignIn(ctx, s.email, password)
	switch {
	case err == nil:
		return tok, nil
	case Refused(err):
		// Both credentials are gone: the refresh token is dead and the password no
		// longer works. Nothing fft can do unattended will fix that.
		return Token{}, &ReauthError{Project: s.project, Err: errors.Join(refreshErr, err)}
	default:
		return Token{}, err
	}
}

func (s *FirebaseTokenSource) password() (string, error) {
	pw, err := s.store.Get(secrets.Key(s.project, secrets.KindPassword))
	if errors.Is(err, secrets.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the stored password for %q: %w", s.project, err)
	}
	return pw, nil
}

// expiryLayout is how a token's expiry is stored. RFC 3339 rather than a unix
// count so that a user inspecting their keychain can read it.
const expiryLayout = time.RFC3339

// loadToken reads the cached token from the credential store.
//
// Anything missing or unparseable yields a token that is simply not fresh, which
// costs one refresh — so a corrupted cache entry degrades into a sign-in rather
// than into a failure.
func loadToken(store secrets.Store, project string) Token {
	get := func(kind string) string {
		v, err := store.Get(secrets.Key(project, kind))
		if err != nil {
			return ""
		}
		return v
	}

	tok := Token{
		ID:      get(secrets.KindIDToken),
		Refresh: get(secrets.KindRefreshToken),
	}
	if exp, err := time.Parse(expiryLayout, get(secrets.KindIDTokenExp)); err == nil {
		tok.ExpiresAt = exp
	}
	return tok
}

// saveToken persists a freshly minted token.
//
// A read-only store is not an error: in CI the credentials come from the
// environment and there is nowhere durable to put a token. The job signs in once
// per process, which is the correct behaviour and not a failure to report.
func saveToken(store secrets.Store, project string, tok Token) error {
	entries := map[string]string{
		secrets.KindIDToken:      tok.ID,
		secrets.KindRefreshToken: tok.Refresh,
		secrets.KindIDTokenExp:   tok.ExpiresAt.UTC().Format(expiryLayout),
	}

	for kind, value := range entries {
		err := store.Set(secrets.Key(project, kind), value)
		if errors.Is(err, secrets.ErrReadOnly) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("cache the %s for %q: %w", kind, project, err)
		}
	}
	return nil
}
