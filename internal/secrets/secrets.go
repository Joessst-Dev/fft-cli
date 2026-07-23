// Package secrets stores the per-project credentials that must never land in
// the config file: passwords, refresh tokens and cached id tokens.
//
// The OS keychain is the default backing store, but it cannot be the only one.
// A GitHub Linux runner has no Secret Service, so go-keyring fails there
// outright; a headless Linux desktop may have none either. Store is therefore
// an interface with four implementations, selected at startup.
package secrets

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound is returned by Get when the key holds no secret. Every
// implementation normalises its backend's "missing" error to this one so that
// callers can branch on it without knowing which store they got.
var ErrNotFound = errors.New("secret not found")

// ErrReadOnly is returned by Set and Delete on a store that cannot be written,
// such as the environment-backed store used in CI.
var ErrReadOnly = errors.New("secret store is read-only")

// Store keeps a secret per key. Keys are built with [Key]; a store must never
// interpret them.
type Store interface {
	// Get returns the secret at key, or ErrNotFound if there is none.
	Get(key string) (string, error)
	// Set writes val to key, replacing any previous value.
	Set(key, val string) error
	// Delete removes key. Deleting a key that holds no secret is not an error.
	Delete(key string) error
	// Kind names the backing store for display: "keyring", "env", "file" or
	// "memory".
	Kind() string
}

// Kinds of secret held per project. Each one is stored under its own key.
//
// They are deliberately not bundled into a single JSON blob: the Windows
// Credential Manager caps one credential at roughly 2.5 KB and a Firebase id
// token alone is about 1 KB, so a combined blob would silently fail to save on
// Windows.
const (
	KindPassword     = "password"
	KindRefreshToken = "refreshToken"
	KindIDToken      = "idToken"
	KindIDTokenExp   = "idTokenExp"
	// KindAPIKey is the Firebase Web API key. It confers no authorization by
	// itself and only ever travels to Google's identity endpoints, but it is kept
	// out of the plaintext config file and held here alongside the credentials all
	// the same.
	KindAPIKey = "apiKey"
)

// AllKinds lists every secret kind a project may have. `fft project remove`
// walks it to make sure nothing is left behind in the keychain.
func AllKinds() []string {
	return []string{KindPassword, KindRefreshToken, KindIDToken, KindIDTokenExp, KindAPIKey}
}

// Key builds the storage key for one secret of one project, for example
// "fft:staging:refreshToken".
func Key(project, kind string) string {
	return "fft:" + project + ":" + kind
}

// ParseKey splits a key produced by [Key] back into its parts.
func ParseKey(key string) (project, kind string, ok bool) {
	rest, ok := strings.CutPrefix(key, "fft:")
	if !ok {
		return "", "", false
	}
	project, kind, ok = strings.Cut(rest, ":")
	if !ok || project == "" || kind == "" {
		return "", "", false
	}
	return project, kind, true
}

// DeleteAll removes every secret belonging to project. It keeps going after a
// failure so that one unreadable entry cannot strand the rest, and reports all
// the failures together.
func DeleteAll(s Store, project string) error {
	var errs []error
	for _, kind := range AllKinds() {
		if err := s.Delete(Key(project, kind)); err != nil && !errors.Is(err, ErrNotFound) {
			errs = append(errs, fmt.Errorf("delete %s: %w", kind, err))
		}
	}
	return errors.Join(errs...)
}

// Has reports whether the project has any credential at all in the store — a
// password to sign in with, or an id token to use directly.
func Has(s Store, project string) bool {
	for _, kind := range []string{KindPassword, KindIDToken} {
		if v, err := s.Get(Key(project, kind)); err == nil && v != "" {
			return true
		}
	}
	return false
}
