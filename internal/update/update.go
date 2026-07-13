// Package update reports whether a newer fft release is available.
//
// It is deliberately a best-effort background chore: nothing it does may delay
// or fail a command. The result of the last check is cached for [Interval], so
// the network is consulted at most once a day, and every failure — no network,
// a rate limit, a repository with no releases yet — is swallowed.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
)

const (
	// DefaultURL is GitHub's latest-release endpoint for fft. It is
	// unauthenticated: the rate limit it shares with the rest of the machine is
	// 60 requests an hour, and one a day per user is comfortably inside it.
	DefaultURL = "https://api.github.com/repos/Joessst-Dev/fft-cli/releases/latest"

	// Interval is how long a cached answer is trusted before GitHub is asked
	// again.
	Interval = 24 * time.Hour

	// Timeout bounds a single check. It is short on purpose: the check runs
	// alongside the command, and a user on a plane must not wait for it.
	Timeout = 1500 * time.Millisecond

	// maxBody caps what is read from GitHub. A release payload is a few
	// kilobytes; anything larger is not a release payload.
	maxBody = 1 << 20
)

// State is what the cache file holds: the outcome of the last check.
//
// CheckedAt is stamped even when the check failed, which is the point of it —
// without it a user with no network would ask GitHub on every single invocation.
type State struct {
	CheckedAt     time.Time `json:"checkedAt"`
	LatestVersion string    `json:"latestVersion,omitempty"`
	URL           string    `json:"url,omitempty"`
}

// Checker asks GitHub for the latest release and remembers the answer.
//
// The zero value is not usable; call [New]. A Checker is safe to use from the
// background goroutine that owns it, and holds no state beyond its
// configuration — the cache lives in a file.
type Checker struct {
	version   string
	cachePath string
	url       string
	client    *http.Client
	now       func() time.Time
}

// Option configures a [Checker].
type Option func(*Checker)

// WithURL replaces the release endpoint. Specs point it at an httptest server;
// production leaves it at [DefaultURL].
func WithURL(url string) Option {
	return func(c *Checker) { c.url = url }
}

// WithClock replaces time.Now, so that a spec can age the cache without
// sleeping.
func WithClock(now func() time.Time) Option {
	return func(c *Checker) { c.now = now }
}

// New returns a Checker for the running fft version, caching its answer in the
// file at cachePath.
//
// version is the fft build's own version, used both as the User-Agent GitHub
// asks callers to send and as the left-hand side of the version comparison.
func New(version, cachePath string, opts ...Option) *Checker {
	c := &Checker{
		version:   version,
		cachePath: cachePath,
		url:       DefaultURL,

		// Its own client: the API client carries a bearer token and a tenant base
		// URL, and neither has any business reaching GitHub.
		//
		// It carries no deadline of its own, deliberately. The caller's context
		// bounds the request — [Timeout] for the background check, --timeout for
		// `fft update check` — and a deadline baked in here would silently overrule
		// both.
		client: &http.Client{},
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Current is the fft version this Checker compares releases against.
func (c *Checker) Current() string { return c.version }

// Cached returns the last stored result, and whether it is fresh enough to use
// instead of asking GitHub again.
//
// A missing, unreadable or corrupt cache file is reported as a zero State that
// is not fresh: a cache we cannot understand is one we do not have. A CheckedAt
// in the future is not fresh either — a clock that jumped must not silence the
// check forever.
func (c *Checker) Cached() (State, bool) {
	data, err := os.ReadFile(c.cachePath)
	if err != nil {
		return State{}, false
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, false
	}

	age := c.now().Sub(s.CheckedAt)
	return s, !s.CheckedAt.IsZero() && age >= 0 && age < Interval
}

// Claim records that a check is being made, before it is made.
//
// The process does not necessarily outlive the check. A command that finishes in
// milliseconds exits while the request to GitHub is still in flight, and the
// background goroutine dies with it, having written nothing at all — so the next
// invocation would find the same stale cache and ask again, and so would the one
// after that. A user whose commands are fast, or whose network is simply down,
// would hit GitHub every single time they typed fft.
//
// Stamping the cache first costs one small write a day and makes that
// impossible. What was already known is carried over: the claim records when we
// asked, not what we learned.
func (c *Checker) Claim() error {
	prev, _ := c.Cached()
	return c.save(State{CheckedAt: c.now(), LatestVersion: prev.LatestVersion, URL: prev.URL})
}

// Refresh asks GitHub for the latest release, ignoring the cache's age, and
// stores what it learns.
//
// The cache is stamped with the current time even when the request fails, so
// that a broken network costs one request a day rather than one per command. On
// failure the previously known release is kept: it is old information, but it is
// not wrong, and dropping it would silence the notice for a day.
func (c *Checker) Refresh(ctx context.Context) (State, error) {
	prev, _ := c.Cached()

	latest, err := c.fetch(ctx)
	if err != nil {
		stamped := State{CheckedAt: c.now(), LatestVersion: prev.LatestVersion, URL: prev.URL}
		// The write's own error is beneath mention: the caller is already being told
		// the check failed, and which of the two reasons it failed for changes
		// nothing it could do.
		_ = c.save(stamped)
		return stamped, err
	}

	if err := c.save(latest); err != nil {
		return latest, err
	}
	return latest, nil
}

// release is the sliver of GitHub's release payload fft needs.
type release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func (c *Checker) fetch(ctx context.Context) (State, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return State{}, fmt.Errorf("build the release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "fft/"+c.version)

	res, err := c.client.Do(req)
	if err != nil {
		return State{}, fmt.Errorf("ask GitHub for the latest release: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		// A 404 is the ordinary answer for a repository that has never been
		// released, and a 403 is the rate limit. Both are "no answer today".
		return State{}, fmt.Errorf("ask GitHub for the latest release: %s", res.Status)
	}

	var rel release
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBody)).Decode(&rel); err != nil {
		return State{}, fmt.Errorf("decode GitHub's release: %w", err)
	}
	if rel.TagName == "" {
		return State{}, errors.New("GitHub returned a release with no tag")
	}

	return State{CheckedAt: c.now(), LatestVersion: rel.TagName, URL: rel.HTMLURL}, nil
}

// save writes the cache file, 0600 in a 0700 directory, atomically — a command
// killed mid-write leaves the previous cache intact rather than a half-written
// one that the next run would throw away.
func (c *Checker) save(s State) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode the update cache: %w", err)
	}
	if err := atomicfile.Write(c.cachePath, data); err != nil {
		return fmt.Errorf("write %s: %w", c.cachePath, err)
	}
	return nil
}

// Notice is the line to show the user, or "" when there is nothing to say —
// fft is current, the release is older, or either version is not a version we
// can compare.
func (c *Checker) Notice(s State) string {
	if !Newer(c.version, s.LatestVersion) {
		return ""
	}
	return fmt.Sprintf("⚡ fft %s is available (you have %s) — brew upgrade fft",
		canonical(s.LatestVersion), canonical(c.version))
}

// Comparable reports whether v has a place in the version ordering at all — that
// is, whether it is a real semver version rather than "dev", "", or a branch
// name.
//
// A build that is not comparable is never told to upgrade, and must never be
// told it is up to date either: the honest answer about it is "unknown".
func Comparable(v string) bool { return semver.IsValid(canonical(v)) }

// Newer reports whether latest is a later release than current.
//
// The comparison is a real semver parse, because the obvious string comparison
// gets v1.10.0 vs v1.9.0 exactly backwards. Anything that is not a semver
// version — "dev", "", a branch name — compares as no answer at all, which is
// what suppresses the notice on a local build.
func Newer(current, latest string) bool {
	if !Comparable(current) || !Comparable(latest) {
		return false
	}
	return semver.Compare(canonical(latest), canonical(current)) > 0
}

// canonical gives a version the leading "v" that x/mod/semver requires. GitHub
// tags carry it by convention but nothing enforces that, and a ldflags-stamped
// version may or may not.
func canonical(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// DefaultCachePath is $XDG_CACHE_HOME/fft/update.json, falling back to
// ~/.cache/fft/update.json.
func DefaultCachePath() (string, error) {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "fft", "update.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "fft", "update.json"), nil
}
