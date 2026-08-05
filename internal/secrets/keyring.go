package secrets

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// service is the name fft registers under in the OS keychain. Every key lives
// beneath it, so a user can find and revoke fft's credentials in one place.
const service = "fft"

// keyringStore keeps secrets in the OS keychain: Keychain on macOS, the Secret
// Service (via D-Bus) on Linux, the Credential Manager on Windows.
//
// The three calls into go-keyring are fields rather than direct calls because
// there is no other way to exercise a backend that is not there: upstream's mock
// is process-global and cannot be made to fail one operation.
type keyringStore struct {
	get func(service, user string) (string, error)
	set func(service, user, pass string) error
	// remove, not delete, which is a builtin.
	remove func(service, user string) error
}

// NewKeyring returns a Store backed by the OS keychain.
func NewKeyring() Store { return keyringStore{keyring.Get, keyring.Set, keyring.Delete} }

func (keyringStore) Kind() string { return "keyring" }

func (s keyringStore) Get(key string) (string, error) {
	val, err := s.get(service, key)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return "", ErrNotFound
	case unavailable(err):
		return "", &unavailableError{err}
	case err != nil:
		return "", fmt.Errorf("read %q from the keychain: %w", key, err)
	}
	return val, nil
}

func (s keyringStore) Set(key, val string) error {
	err := s.set(service, key, val)
	switch {
	case err == nil:
		return nil
	// Which key we were trying to write is beside the point when there is nothing
	// there to write it to, so the per-operation wrapper is dropped here.
	case unavailable(err):
		return &unavailableError{err}
	default:
		return fmt.Errorf("write %q to the keychain: %w", key, err)
	}
}

func (s keyringStore) Delete(key string) error {
	err := s.remove(service, key)
	switch {
	case err == nil, errors.Is(err, keyring.ErrNotFound):
		return nil
	case unavailable(err):
		return &unavailableError{err}
	default:
		return fmt.Errorf("delete %q from the keychain: %w", key, err)
	}
}
