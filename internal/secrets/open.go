package secrets

import "fmt"

// Open returns the Store implied by the environment.
//
// The keychain is the default, because it is the only place a secret is
// protected by something other than a file mode. noKeyring (--no-keyring, or
// FFT_NO_KEYRING=1) selects the 0600 file fallback instead, for machines where
// there is no keychain to talk to.
//
// The environment-backed store is not reachable from here: it belongs to the
// ephemeral project synthesized in headless mode, which the caller detects
// before it ever asks for a store.
func Open(noKeyring bool) (Store, error) {
	if !noKeyring {
		return NewKeyring(), nil
	}

	path, err := DefaultFilePath()
	if err != nil {
		return nil, fmt.Errorf("locate the credentials file: %w", err)
	}
	return NewFile(path), nil
}
