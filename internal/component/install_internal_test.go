package component

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAllowedURL(t *testing.T) {
	i := &Installer{api: "https://api.github.com"}

	ok := []string{
		"https://api.github.com/repos/o/r/releases/latest",
		"https://github.com/o/r/releases/download/v1/asset.tar.gz",
		"https://codeload.github.com/o/r/tar.gz/v1",
		"https://objects.githubusercontent.com/x",
		"https://release-assets.githubusercontent.com/x",
	}
	for _, u := range ok {
		if err := i.allowedURL(u); err != nil {
			t.Errorf("allowedURL(%q) = %v, want nil", u, err)
		}
	}

	bad := []string{
		"http://github.com/o/r/asset.tar.gz",       // not https
		"https://evil.example/asset.tar.gz",        // not a GitHub host
		"https://githubusercontent.com.evil.com/x", // suffix spoof
		"https://notgithub.com/o/r/asset",          // lookalike
		// The configured endpoint is api.github.com over *https*; http to that same
		// host must not be waved through by the endpoint exemption.
		"http://api.github.com/repos/o/r/releases/latest",
		"http://API.GitHub.com/x",
	}
	for _, u := range bad {
		if err := i.allowedURL(u); err == nil {
			t.Errorf("allowedURL(%q) = nil, want error", u)
		}
	}
}

// TestGetRefusesRedirectOffAllowedHost proves the redirect re-check: the initial
// URL is the configured (allowed) host, but the server 302s to a disallowed one.
// Without CheckRedirect the client would follow it transparently.
func TestGetRefusesRedirectOffAllowedHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/payload", http.StatusFound)
	}))
	defer srv.Close()

	i := NewInstaller(t.TempDir(), WithAPI(srv.URL))
	_, err := i.get(context.Background(), srv.URL+"/asset", maxArchive, "")
	if err == nil {
		t.Fatal("a redirect to a non-GitHub host was followed")
	}
}

// TestAllowedURLConfiguredEndpoint proves the WithAPI seam: a spec's loopback
// httptest server serves both the release JSON and the assets over http, and the
// allowlist must let that host through without pinning it to GitHub.
func TestAllowedURLConfiguredEndpoint(t *testing.T) {
	i := &Installer{api: "http://127.0.0.1:54321"}
	if err := i.allowedURL("http://127.0.0.1:54321/assets/weather.tar.gz"); err != nil {
		t.Errorf("configured endpoint refused: %v", err)
	}
	if err := i.allowedURL("http://127.0.0.1:9999/assets/x"); err == nil {
		t.Error("a different http host was allowed; only the configured endpoint may be")
	}
}

func TestIsRegularFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()

	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isRegularFile(real) {
		t.Error("a real executable should be a regular file")
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	if isRegularFile(link) {
		t.Error("a symlink standing in for the executable must not be treated as regular")
	}
}
