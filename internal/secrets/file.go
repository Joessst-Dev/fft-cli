package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
)

// fileStore keeps secrets in a 0600 JSON file. It is the fallback for machines
// with no working keychain — a Linux desktop without a Secret Service, say.
//
// The secrets are stored in cleartext. There is no key to encrypt them with
// that would not itself have to be stored next to them, so pretending otherwise
// would be theatre; the honest protection is file mode 0600 and an explicit
// opt-in (--no-keyring / FFT_NO_KEYRING=1) so nobody lands here by accident.
//
// On Windows that first half is weaker than it reads. Windows has no POSIX mode
// bits: os.Chmod only toggles the read-only attribute, file security is an ACL,
// and this store sets no ACL of its own — so the file is protected by whatever
// it inherits from its parent directory. Under the default %USERPROFILE% that
// inheritance is sound; point XDG_STATE_HOME at a shared directory and it is
// not, where 0600 on Linux would still hold. See the README section "On Windows,
// --no-keyring protects less than 0600 suggests".
type fileStore struct {
	path string

	// Reads and writes are read-modify-write over the whole file, so they are
	// serialised. This guards one process against itself; two concurrent fft
	// processes are still last-write-wins, which is acceptable for a CLI.
	mu sync.Mutex
}

// NewFile returns a Store backed by the JSON file at path. The file and its
// parent directory are created on first write, with modes 0600 and 0700.
func NewFile(path string) Store {
	return &fileStore{path: path}
}

// DefaultFilePath is the credentials file fft falls back to when the keychain is
// unavailable: $XDG_STATE_HOME/fft/credentials.json, or ~/.local/state/fft/
// credentials.json when XDG_STATE_HOME is unset.
func DefaultFilePath() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "fft", "credentials.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "fft", "credentials.json"), nil
}

func (*fileStore) Kind() string { return "file" }

func (s *fileStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	values, err := s.load()
	if err != nil {
		return "", err
	}

	val, ok := values[key]
	if !ok {
		return "", ErrNotFound
	}
	return val, nil
}

func (s *fileStore) Set(key, val string) error {
	return s.update(func(values map[string]string) { values[key] = val })
}

func (s *fileStore) Delete(key string) error {
	return s.update(func(values map[string]string) { delete(values, key) })
}

func (s *fileStore) update(mutate func(map[string]string)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	values, err := s.load()
	if err != nil {
		return err
	}
	mutate(values)
	return s.save(values)
}

// load reads the file. A missing file is an empty set of secrets, not an error.
func (s *fileStore) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return make(map[string]string), nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}

	values := make(map[string]string)
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	return values, nil
}

func (s *fileStore) save(values map[string]string) error {
	data, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	return atomicfile.Write(s.path, data)
}
