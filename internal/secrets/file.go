package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

	// warnOnce keeps the loose-permissions warning to one line per process, since
	// load runs on every read. warn receives it; nil sends it to stderr, where every
	// other notice goes. A spec replaces warn to capture it.
	warnOnce sync.Once
	warn     func(string)
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
	return s.update(func(values map[string]string) bool {
		values[key] = val
		return true
	})
}

func (s *fileStore) Delete(key string) error {
	return s.update(func(values map[string]string) bool {
		if _, ok := values[key]; !ok {
			return false
		}
		delete(values, key)
		return true
	})
}

// update applies mutate and saves, unless mutate reports it changed nothing.
//
// The nothing-changed case is not an optimisation. Deleting a key that is not
// here must not bring the file into existence: fft asks one store to forget a
// project even when the other store is the one holding it, and creating an empty
// cleartext credentials file on a machine that has a working keychain would be a
// surprising thing for a delete to do.
func (s *fileStore) update(mutate func(map[string]string) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	values, err := s.load()
	if err != nil {
		return err
	}
	if !mutate(values) {
		return nil
	}
	return s.save(values)
}

// load reads the file. A missing file is an empty set of secrets, not an error.
func (s *fileStore) load() (map[string]string, error) {
	// Warn (never refuse) about loose permissions before the read, so the check also
	// covers the first write: load runs before save, and the directory may already
	// exist with an open mode that the write is about to drop a cleartext token into.
	s.warnIfLoose()

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

// warnIfLoose emits, at most once, a warning when the credentials file or its
// parent directory is group- or world-accessible. A missing file or directory is
// fine — there is nothing to leak yet.
func (s *fileStore) warnIfLoose() {
	// Windows has no POSIX mode bits: os.Stat there synthesizes 0666 for a file and
	// 0777 for a directory, so this check would fire on every single invocation. The
	// Credential Manager is the real protection on Windows, and the 0600 story does
	// not apply — see the README section on --no-keyring on Windows.
	if runtime.GOOS == "windows" {
		return
	}
	s.warnOnce.Do(func() {
		if err := atomicfile.CheckPrivate(s.path); errors.Is(err, atomicfile.ErrNotPrivate) {
			// A file: chmod 600 is the fix.
			s.emitWarning(err.Error(), "chmod 600")
		}
		dir := filepath.Dir(s.path)
		if info, err := os.Stat(dir); err == nil && info.Mode().Perm()&0o077 != 0 {
			// A directory needs its execute/traverse bit, so 700 — never 600, which
			// would lock the owner out of their own credentials directory.
			s.emitWarning(fmt.Sprintf("%s has mode %#o and is reachable by other users", dir, info.Mode().Perm()), "chmod 700")
		}
	})
}

// emitWarning routes a warning to the store's sink, or to stderr — where notices
// belong, so a --debug-free `-o json | jq` is never contaminated by it. fix is the
// concrete command that closes the hole (chmod 600 for the file, 700 for the dir).
func (s *fileStore) emitWarning(problem, fix string) {
	msg := "warning: " + problem + "; your credentials are stored there in cleartext — run " + fix + " on it"
	if s.warn != nil {
		s.warn(msg)
		return
	}
	fmt.Fprintln(os.Stderr, msg)
}

func (s *fileStore) save(values map[string]string) error {
	data, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	return atomicfile.Write(s.path, data)
}
