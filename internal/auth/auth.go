// Package auth mints the bearer tokens fft sends to fulfillmenttools.
//
// fulfillmenttools authenticates nobody. Its swagger declares one security
// scheme — a bearer JWT — and contains no login endpoint at all. Sign-in happens
// out of band against Google Identity Platform (Firebase), and only the id token
// that comes back is ever shown to a tenant.
//
// # The API key
//
// The Firebase *Web* API key identifies the Firebase project and grants nothing
// by itself. It belongs on Google's two identity endpoints as ?key=, and nowhere
// else. [Client] therefore owns its own *http.Client whose transport refuses
// every host but those two, so the key is structurally incapable of reaching a
// fulfillmenttools tenant — a leak no amount of care at the call site could
// reliably prevent. It is nonetheless held as sensitive data: kept in the secret
// store, never in the plaintext config file.
//
// # Two responses, two shapes
//
// Sign-in answers in camelCase (idToken, refreshToken, expiresIn); refresh
// answers in snake_case (id_token, refresh_token, expires_in). Both are decoded
// by their own struct, because one struct for both unmarshals *cleanly* into an
// empty token — the failure would surface as a 401 on the next request rather
// than as a decode error here. In both shapes the expiry is a JSON string, not a
// number.
package auth

import (
	"context"
	"time"
)

// Leeway is how much of an id token's life fft refuses to rely on. A token with
// less than this left is refreshed before it is used, so that a request cannot
// expire in flight.
const Leeway = 5 * time.Minute

// TokenSource yields the bearer token for the fulfillmenttools API.
//
// It is the seam a future machine-to-machine or OIDC mode plugs into: nothing
// outside this package knows that today's token comes from a password sign-in.
type TokenSource interface {
	// Token returns an id token that is valid now, minting or refreshing one if
	// it has to.
	Token(ctx context.Context) (string, error)
}

// Renewer is a TokenSource that can be forced to mint a fresh token, which is
// what `fft auth refresh` does. A [StaticTokenSource] is not one: FFT_ID_TOKEN is
// a fixed string with nothing behind it to renew from.
type Renewer interface {
	Renew(ctx context.Context) (Token, error)
}

// Token is a minted id token, what it takes to renew it, and when it dies.
type Token struct {
	// ID is the JWT sent as `Authorization: Bearer …`.
	ID string
	// Refresh renews the id token without the password. It is long-lived, so it
	// is as sensitive as the password itself.
	Refresh string
	// ExpiresAt is when ID stops being accepted.
	ExpiresAt time.Time
	// Email is the address that authenticated. Google reports it on sign-in and
	// not on refresh, so it is carried forward rather than re-derived.
	Email string
}

// Fresh reports whether the token can still be used at now, with leeway to
// spare.
func (t Token) Fresh(now time.Time, leeway time.Duration) bool {
	return t.ID != "" && now.Add(leeway).Before(t.ExpiresAt)
}

// staticTokenSource serves a token somebody else minted.
type staticTokenSource string

// StaticTokenSource returns a TokenSource that always yields token.
//
// It backs FFT_ID_TOKEN: a CI job that has already signed in elsewhere hands fft
// the token and nothing else. There is no password and no refresh token behind
// it, so when it expires the only honest thing to do is fail.
func StaticTokenSource(token string) TokenSource { return staticTokenSource(token) }

func (s staticTokenSource) Token(context.Context) (string, error) { return string(s), nil }
