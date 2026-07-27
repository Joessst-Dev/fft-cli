package secrets

import (
	"os"
	"path/filepath"
	"runtime"
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

	// A read-mostly store must not repeat the warning on every read.
	before := len(warnings)
	if _, err := s.Get("fft:staging:password"); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != before {
		t.Fatalf("warned again on the second read: %d -> %d", before, len(warnings))
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
