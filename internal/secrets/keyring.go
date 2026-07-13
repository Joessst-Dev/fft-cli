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
type keyringStore struct{}

// NewKeyring returns a Store backed by the OS keychain.
func NewKeyring() Store { return keyringStore{} }

func (keyringStore) Kind() string { return "keyring" }

func (keyringStore) Get(key string) (string, error) {
	val, err := keyring.Get(service, key)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return "", ErrNotFound
	case err != nil:
		return "", fmt.Errorf("read %q from the keychain: %w", key, err)
	}
	return val, nil
}

func (keyringStore) Set(key, val string) error {
	if err := keyring.Set(service, key, val); err != nil {
		return fmt.Errorf("write %q to the keychain: %w", key, err)
	}
	return nil
}

func (keyringStore) Delete(key string) error {
	err := keyring.Delete(service, key)
	switch {
	case err == nil, errors.Is(err, keyring.ErrNotFound):
		return nil
	default:
		return fmt.Errorf("delete %q from the keychain: %w", key, err)
	}
}
