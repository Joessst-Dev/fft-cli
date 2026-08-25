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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
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

// askOp names the thing that failed, in the words the user typed it: they asked
// fft to ask GitHub. Every message about a failed check opens with it.
const askOp = "ask GitHub for the latest release"

// Error is a check GitHub did not answer.
//
// Its whole job is to carry an exit code and, where GitHub said enough for one, a
// remedy. Every failure of the check gets one — a status, a network that is not
// there, a body that is not a release — because the answer to all of them is the
// same: nothing here is broken, ask again later.
type Error struct {
	// Status is what GitHub answered, or 0 when the request never got an answer.
	Status int

	// Limit and Reset are GitHub's own account of the hourly budget, and are set
	// only when that budget is what failed the check. Either may be absent: they
	// are read from headers, and a header is not a promise.
	Limit int
	Reset time.Time

	rateLimited bool
	msg         string
	err         error
}

// wrap builds an [Error] around a cause, which may be nil.
//
// The message is a Sprintf, not an Errorf: %w does not work here, and a call site
// that reaches for it out of habit gets %!w(...) printed at the user rather than a
// wrapped error. Pass the cause twice — once to wrap, once as %s — because
// Unwrap's copy is the one errors.Is walks.
func wrap(cause error, format string, args ...any) *Error {
	return &Error{msg: fmt.Sprintf(format, args...), err: cause}
}

func (e *Error) Error() string { return e.msg }

// Unwrap keeps the cause reachable, which is load-bearing for context.Canceled:
// [exitcode.FromError] tests for it before it asks an error for its own code, so
// without this a Ctrl-C during the check would exit 9 instead of 130.
func (e *Error) Unwrap() error { return e.err }

// ExitCode is [exitcode.Unavailable] for every failed check, whatever failed it.
// Not Forbidden, even for a 403: that code means the *tenant* refused an
// authenticated request, and a script that reacts to it by trying other
// credentials would be reacting to the wrong thing entirely. This is GitHub
// declining to answer a question about a release, and the only useful response to
// it is to ask again later.
func (e *Error) ExitCode() int { return exitcode.Unavailable }

// RateLimited reports whether GitHub refused because the hour's budget is spent,
// rather than for a reason of its own.
func (e *Error) RateLimited() bool { return e.rateLimited }

// Hint is where the remedy goes, on its own line under the message.
//
// It names the thing a user cannot guess: the budget is not fft's, and fft asking
// once a day is very unlikely to be what spent it. FFT_NO_UPDATE_CHECK is offered
// for the machine where this is chronic — it silences the *background* check, not
// this command, which always asks.
func (e *Error) Hint() string {
	if !e.RateLimited() {
		return ""
	}
	return "The limit is shared with every unauthenticated caller on this address — gh, another tool's update check, a NAT'd network — so fft asking once a day is unlikely to be what spent it. Wait for the reset, or set FFT_NO_UPDATE_CHECK=1 to stop the background check from asking at all."
}

// release is the sliver of GitHub's release payload fft needs.
type release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func (c *Checker) fetch(ctx context.Context) (State, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return State{}, wrap(err, "build the release request: %s", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "fft/"+c.version)

	res, err := c.client.Do(req)
	if err != nil {
		return State{}, wrap(err, "%s: %s", askOp, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return State{}, c.statusError(res)
	}

	var rel release
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBody)).Decode(&rel); err != nil {
		return State{}, wrap(err, "decode GitHub's release: %s", err)
	}
	if rel.TagName == "" {
		return State{}, wrap(nil, "GitHub returned a release with no tag")
	}

	return State{CheckedAt: c.now(), LatestVersion: rel.TagName, URL: rel.HTMLURL}, nil
}

// statusError explains a status GitHub answered with, as far as its headers allow.
//
// The rate limit is the case worth spelling out, and the common one: the check is
// unauthenticated, so it draws on a budget of 60 requests an hour that belongs to
// the machine's IP address rather than to fft. gh, another tool's own update check
// or a busy office NAT can spend the hour before fft asks once — and reported as
// nothing but its status, that reads "403 Forbidden", which sends the user looking
// for a permission they never needed.
func (c *Checker) statusError(res *http.Response) error {
	e := &Error{Status: res.StatusCode}

	if !rateLimited(res) {
		// A 404 is the ordinary answer for a repository that has never been
		// released, and a 403 with requests still in hand really is a refusal. There
		// the status is the news.
		e.msg = fmt.Sprintf("%s: %s", askOp, res.Status)
		return e
	}

	e.rateLimited = true
	e.Limit = headerInt(res, "X-RateLimit-Limit")
	switch secs := headerInt(res, "X-RateLimit-Reset"); {
	case secs > 0:
		e.Reset = time.Unix(int64(secs), 0)
	default:
		// The secondary limit names no reset — it carries Retry-After instead, as a
		// number of seconds to wait rather than a moment to wait for.
		if after := headerInt(res, "Retry-After"); after > 0 {
			e.Reset = c.now().Add(time.Duration(after) * time.Second)
		}
	}
	e.msg = fmt.Sprintf("%s: %s", askOp, c.rateLimitReason(e))
	return e
}

// rateLimitReason is the sentence that replaces "403 Forbidden", with whatever of
// GitHub's account of the limit actually arrived. A header that is missing or
// unparseable costs its clause and nothing else: an invented reset time is worse
// than none, because the user would wait for it.
func (c *Checker) rateLimitReason(e *Error) string {
	var details []string
	if e.Limit > 0 {
		details = append(details, fmt.Sprintf("%d unauthenticated requests an hour", e.Limit))
	}
	if !e.Reset.IsZero() {
		if in := e.Reset.Sub(c.now()); in > 0 {
			details = append(details, "resets in "+retryPhrase(in))
		}
	}

	// True of both limits, and of the one detail that matters either way: the
	// budget belongs to the address, not to fft.
	reason := "GitHub is rate limiting this IP address"
	if len(details) == 0 {
		return reason
	}
	return reason + " (" + strings.Join(details, ", ") + ")"
}

// retryPhrase is how long until the limit lifts, at the precision the user can
// act on: a countdown to the second would be stale by the time it is read.
func retryPhrase(d time.Duration) string {
	if d < time.Minute {
		return "under a minute"
	}
	// Rounded once, up front, so the branch below and the number it prints always
	// agree — a reset at 59m31s must not choose the "%dm" branch on the unrounded
	// duration and then print a rounded-up "60m".
	d = d.Round(time.Minute)
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	// The window is an hour, so this is a clock that disagrees with GitHub's
	// rather than a real wait. Say what the header said anyway.
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// rateLimited reports whether GitHub is holding fft off rather than refusing it.
//
// The two statuses need different evidence. A 429 is GitHub asking for a slower
// pace — the secondary limit, which watches how fast requests arrive rather than
// how many — and this endpoint has no other reason to send one, so it counts on
// its own; the hourly counter it carries is usually untouched. A 403 is both the
// primary limit's status and GitHub's plain refusal, and only the spent counter
// tells those apart. Calling a 403 with requests still in hand a rate limit would
// be #157's mistake in the other direction.
func rateLimited(res *http.Response) bool {
	switch res.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return strings.TrimSpace(res.Header.Get("X-RateLimit-Remaining")) == "0"
	default:
		return false
	}
}

// headerInt reads one of GitHub's numeric rate-limit headers, or 0 when it is
// absent, is not a number, or is negative. Every caller treats 0 as "not said" —
// which is the whole reason a negative is folded in with the rest: a budget of
// "-1 requests an hour", or a reset before the epoch, is a header fft has no
// business repeating at the user, and dropping the clause says less but says only
// true things.
func headerInt(res *http.Response, name string) int {
	n, err := strconv.Atoi(strings.TrimSpace(res.Header.Get(name)))
	if err != nil || n < 0 {
		return 0
	}
	return n
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
