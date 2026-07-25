package component

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// The compiled-in first-party table and the committed component.yaml files are two
// copies of the same facts: the table is authoritative for the command tree, the
// files are what the release archive is assembled from. Two copies drift, so this is
// the gate that says they have not.
//
// A plain testing.T rather than a Ginkgo spec, because it reads files by a path
// relative to this source file and has nothing to do with the behavioural suite.
func TestFirstPartyMatchesCommittedManifests(t *testing.T) {
	root := repoRoot(t)

	for _, want := range firstParty {
		path := filepath.Join(root, "components", want.Name, ManifestName)

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: no committed manifest (%v); the release has nothing to ship", want.Name, err)
			continue
		}

		got, err := ParseManifest(data, path)
		if err != nil {
			t.Errorf("%s: committed manifest does not parse: %v", want.Name, err)
			continue
		}

		// The fields the release depends on. Not the command help — the table owns that,
		// and the file need not repeat prose that would only drift.
		if got.Exec != want.Exec {
			t.Errorf("%s: exec is %q in the table and %q in the manifest", want.Name, want.Exec, got.Exec)
		}
		if got.Kind != want.Kind {
			t.Errorf("%s: kind is %q in the table and %q in the manifest", want.Name, want.Kind, got.Kind)
		}
		// Env is load-bearing at runtime: it is what a component may ask fft to forward
		// (FFT_BASE_URL) and what the emulator checks to know a transport is configured.
		// A table and a manifest that disagree on it behave differently installed vs.
		// compiled-in, which is exactly the drift this test exists to catch.
		if !slices.Equal(got.Env, want.Env) {
			t.Errorf("%s: env is %v in the table and %v in the manifest", want.Name, want.Env, got.Env)
		}
	}
}

// TestCommittedManifestsAreValid parses every committed component.yaml, not only the
// ones the compiled-in table names.
//
// The table has just the emulator, but goreleaser and the Dockerfile ship the two
// transport manifests too — and a transport is not in firstParty, so the drift test
// above never touches emulator-pubsub or emulator-servicebus. A typo in one of those
// would sail through every gate and ship a component fft's own installer then refuses.
func TestCommittedManifestsAreValid(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "components")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	found := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), ManifestName)

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
			continue
		}
		m, err := ParseManifest(data, path)
		if err != nil {
			t.Errorf("%s: committed manifest does not parse: %v", entry.Name(), err)
			continue
		}
		// The directory a manifest ships in is the name the installer keys on, so the
		// two must agree — the same check Registry.Open makes on disk.
		if m.Name != entry.Name() {
			t.Errorf("%s: manifest names it %q", entry.Name(), m.Name)
		}
		found++
	}

	if found == 0 {
		t.Errorf("no committed manifests under %s", dir)
	}
}

// repoRoot walks up from this source file to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this source file")
	}
	// internal/component/firstparty_internal_test.go → repo root is two directories up.
	return filepath.Dir(filepath.Dir(filepath.Dir(self)))
}
