package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileStoreWarnsOnLoosePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits do not apply on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"fft:staging:password":"s3cret"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	s := &fileStore{path: path, warn: func(m string) { warnings = append(warnings, m) }}

	if _, err := s.Get("fft:staging:password"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("a 0644 credentials file drew no warning")
	}
	// The file remediation is chmod 600 — never a directory-only chmod.
	if !strings.Contains(warnings[0], "chmod 600") {
		t.Fatalf("file warning did not suggest chmod 600: %q", warnings[0])
	}

	// A read-mostly store must not repeat the warning on every read.
	before := len(warnings)
	if _, err := s.Get("fft:staging:password"); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != before {
		t.Fatalf("warned again on the second read: %d -> %d", before, len(warnings))
	}
}

// TestFileStoreWarnsOnLooseDirBeforeFirstWrite covers the first-write path: the
// directory is already world-reachable and the credentials file does not exist yet,
// so the warning has to fire before load reads a file that is not there — otherwise
// `fft project add` would drop the cleartext token into a 0777 dir in silence.
func TestFileStoreWarnsOnLooseDirBeforeFirstWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits do not apply on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "credentials.json") // does not exist yet

	var warnings []string
	s := &fileStore{path: path, warn: func(m string) { warnings = append(warnings, m) }}

	// A missing file is a clean miss (ErrNotFound), but load still runs the
	// permission check on the directory that is about to receive the token.
	if _, err := s.Get("fft:staging:password"); err != nil && !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on a missing file should be a clean miss, got: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("a world-reachable directory drew no warning on the first access")
	}
	// A directory needs its traverse bit, so the fix is chmod 700, never 600.
	if !strings.Contains(warnings[0], "chmod 700") {
		t.Fatalf("directory warning suggested the wrong fix: %q", warnings[0])
	}
}

func TestFileStoreQuietOnPrivateFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits do not apply on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"fft:staging:password":"s3cret"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	s := &fileStore{path: path, warn: func(m string) { warnings = append(warnings, m) }}

	if _, err := s.Get("fft:staging:password"); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("a 0600 file in a 0700 dir drew a warning: %v", warnings)
	}
}
