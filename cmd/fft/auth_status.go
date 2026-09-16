package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

const authStatusLong = `Show what fft has stored to sign in as the current project, without
signing in.

Nothing is sent anywhere and no token is minted: this reads the credential store
and reports what it finds — which store it is, whether a password, a refresh
token and an id token are stored, and when that id token expires. It is the
cheap question to ask before a command that would have to sign in first.

TOKEN is one of:
  valid      the cached id token will be used as it is
  expiring   it has less than five minutes left
  expired    its expiry has passed
  unknown    there is an id token but nothing readable says when it expires
  none       no id token is cached

With a stored password (SIGN-IN "password"), the next command signs in again
when the token is expiring, expired, unknown or none, and fails with exit 4 if
it cannot.

A project running from the environment (FFT_BASE_URL and friends) reports the
"env" store. With FFT_ID_TOKEN and no password, SIGN-IN is "id token": that token
is used as it is and nothing renews it, so once it has expired every command
fails with exit 4. Its expiry is FFT_ID_TOKEN_EXPIRES_AT, or else the token's
own exp claim.

No credential is ever printed, in any output format.`

// Token states reported by `fft auth status`.
const (
	tokenValid    = "valid"
	tokenExpiring = "expiring"
	tokenExpired  = "expired"
	tokenUnknown  = "unknown"
	tokenNone     = "none"
)

// Sign-in modes reported by `fft auth status`: how the next command that needs a
// token would get one. They mirror the branches of [newTokenSource].
const (
	signInPassword = "password"
	signInIDToken  = "idToken"
	signInNone     = "none"
)

// authStatusView is what `fft auth status` renders. It has flags for the secrets
// and no field that could hold one, so no output format can leak a credential.
type authStatusView struct {
	Project  string `json:"project" yaml:"project"`
	Email    string `json:"email,omitempty" yaml:"email,omitempty"`
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	Store    string `json:"store" yaml:"store"`
	SignIn   string `json:"signIn" yaml:"signIn"`

	HasPassword     bool `json:"hasPassword" yaml:"hasPassword"`
	HasRefreshToken bool `json:"hasRefreshToken" yaml:"hasRefreshToken"`
	HasIDToken      bool `json:"hasIdToken" yaml:"hasIdToken"`

	Token string `json:"token" yaml:"token"`

	// Expired is Token == "expired", spelled out so that a script need not know the
	// list of states to ask the one question most of them have.
	Expired   bool       `json:"expired" yaml:"expired"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
	ExpiresIn string     `json:"expiresIn,omitempty" yaml:"expiresIn,omitempty"`
}

func newAuthStatusCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the stored credentials' state, offline",
		Long:  authStatusLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			return runAuthStatus(deps)
		},
	}
}

func runAuthStatus(deps *Deps) error {
	project, err := deps.ActiveProject()
	if err != nil {
		return err
	}

	// Asked without reading the secrets where the store allows it: a status line
	// has no use for a password.
	has := func(kind string) (bool, error) {
		ok, err := secrets.Exists(deps.Secrets, secrets.Key(project.Name, kind))
		if err != nil {
			return false, fmt.Errorf("check the %s for project %q: %w", kind, project.Name, err)
		}
		return ok, nil
	}

	hasPassword, err := has(secrets.KindPassword)
	if err != nil {
		return err
	}
	hasRefresh, err := has(secrets.KindRefreshToken)
	if err != nil {
		return err
	}
	hasID, err := has(secrets.KindIDToken)
	if err != nil {
		return err
	}
	storedExpiry, err := lookupSecret(deps.Secrets, project.Name, secrets.KindIDTokenExp)
	if err != nil {
		return err
	}

	view := authStatusView{
		Project:         project.Name,
		Email:           project.Email,
		Username:        project.Username,
		Store:           deps.Secrets.Kind(),
		HasPassword:     hasPassword,
		HasRefreshToken: hasRefresh,
		HasIDToken:      hasID,
	}

	switch {
	case hasPassword:
		view.SignIn = signInPassword
	case hasID:
		view.SignIn = signInIDToken
	default:
		view.SignIn = signInNone
	}

	if !hasID {
		view.Token = tokenNone
		return deps.Printer.Render(authStatusRows(view), view)
	}

	// The same parse the token cache uses.
	exp, err := time.Parse(time.RFC3339, storedExpiry)
	known := err == nil
	if !known && view.SignIn == signInIDToken {
		// A token used as it is — FFT_ID_TOKEN, minted elsewhere by a CI job — may
		// come with no stored expiry, and its own claim is then the only word on it.
		// With a password the claim is not consulted: the token cache treats an
		// expiry it cannot read as no expiry and signs in again, whatever the token
		// says, so the claim would promise a token the next command will not use.
		idToken, err := lookupSecret(deps.Secrets, project.Name, secrets.KindIDToken)
		if err != nil {
			return err
		}
		exp, known = jwtExpiry(idToken)
	}
	if known {
		view.setExpiry(exp, deps.Clock())
	} else {
		view.Token = tokenUnknown
	}

	return deps.Printer.Render(authStatusRows(view), view)
}

// setExpiry fills in the state of an id token that expires at exp.
func (v *authStatusView) setExpiry(exp, now time.Time) {
	exp = exp.UTC()
	v.ExpiresAt = &exp
	left := exp.Sub(now)
	v.ExpiresIn = left.Round(time.Second).String()

	switch {
	case left <= 0:
		v.Token = tokenExpired
		v.Expired = true
	case left <= auth.Leeway:
		v.Token = tokenExpiring
	default:
		v.Token = tokenValid
	}
}

// maxJWTExpiry is the last second of the year 9999, the latest time JSON and
// YAML can carry as RFC 3339. A claim past it is not a date fft can report.
const maxJWTExpiry = 253402300799

// jwtExpiry reads the exp claim of a JWT without verifying it.
//
// This is for a status line and nothing else, and only for a token used as it is.
// A claim that is missing, malformed, not positive or past the year 9999 is simply
// not reported.
func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, false
	}

	var claims struct {
		Exp json.Number `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, false
	}
	secs, err := claims.Exp.Int64()
	if err != nil || secs <= 0 || secs > maxJWTExpiry {
		return time.Time{}, false
	}
	return time.Unix(secs, 0), true
}

var authStatusHeaders = []string{"PROJECT", "STORE", "SIGN-IN", "STORED", "TOKEN", "EXPIRES AT"}

func authStatusRows(v authStatusView) output.Rows {
	stored := make([]string, 0, 3)
	for _, s := range []struct {
		has  bool
		name string
	}{
		{v.HasPassword, "password"},
		{v.HasRefreshToken, "refresh token"},
		{v.HasIDToken, "id token"},
	} {
		if s.has {
			stored = append(stored, s.name)
		}
	}
	if len(stored) == 0 {
		stored = append(stored, "nothing")
	}

	signIn := map[string]string{
		signInPassword: "password",
		signInIDToken:  "id token",
		signInNone:     "none",
	}[v.SignIn]

	token := v.Token
	expiresAt := ""
	if v.ExpiresAt != nil {
		expiresAt = v.ExpiresAt.Format(time.RFC3339)
		if v.Expired {
			token += " (" + strings.TrimPrefix(v.ExpiresIn, "-") + " ago)"
		} else {
			token += " (" + v.ExpiresIn + " left)"
		}
	}

	return output.Rows{
		Headers: authStatusHeaders,
		Rows: [][]string{{
			v.Project,
			v.Store,
			signIn,
			strings.Join(stored, ", "),
			token,
			expiresAt,
		}},
	}
}
