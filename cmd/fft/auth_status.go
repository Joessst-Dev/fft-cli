package main

import (
	"encoding/base64"
	"encoding/json"
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
  expiring   it has less than five minutes left, so the next command renews it
  expired    the next command renews it, or fails with exit 4 if it cannot
  unknown    there is an id token but nothing says when it expires
  none       no id token is cached; the next command signs in

A project running from the environment (FFT_BASE_URL and friends) reports the
"env" store. With FFT_ID_TOKEN and no password, SIGN-IN is "id token": that token
is used as it is and cannot be renewed.

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

	has := func(kind string) (string, bool, error) {
		v, err := lookupSecret(deps.Secrets, project.Name, kind)
		return v, v != "", err
	}

	_, hasPassword, err := has(secrets.KindPassword)
	if err != nil {
		return err
	}
	_, hasRefresh, err := has(secrets.KindRefreshToken)
	if err != nil {
		return err
	}
	idToken, hasID, err := has(secrets.KindIDToken)
	if err != nil {
		return err
	}
	storedExpiry, _, err := has(secrets.KindIDTokenExp)
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

	view.setToken(idToken, storedExpiry, deps.Clock())

	return deps.Printer.Render(authStatusRows(view), view)
}

// setToken fills in the token's state. The id token itself is only ever read for
// its expiry, and only when the store does not say.
func (v *authStatusView) setToken(idToken, storedExpiry string, now time.Time) {
	if idToken == "" {
		v.Token = tokenNone
		return
	}

	// The same parse the token cache uses: an expiry it cannot read is one it treats
	// as stale, so this reports what the next command will actually do.
	exp, err := time.Parse(time.RFC3339, storedExpiry)
	if err != nil {
		var ok bool
		if exp, ok = jwtExpiry(idToken); !ok {
			v.Token = tokenUnknown
			return
		}
	}

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

// jwtExpiry reads the exp claim of a JWT without verifying it.
//
// This is for a status line and nothing else. The token is only decoded where it
// is — FFT_ID_TOKEN, handed over by a CI job that minted it elsewhere, comes with no
// stored expiry — and a claim that is missing or malformed is simply not reported.
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
	if err != nil || secs <= 0 {
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
