package component

import (
	"os"
	"path/filepath"
	"runtime"
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
