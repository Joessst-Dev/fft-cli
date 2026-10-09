package skill

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
)

// ManifestName is the file in the skill's directory that records what fft wrote
// there.
//
// It is what lets an upgrade tell fft's own old text from the user's edit. Every
// release that changes a word of the skill changes the bytes of an installed copy
// against the new one, and comparing bytes alone cannot say whose change that was
// — so without a record, every upgrade asked for --force, and an agent, which has
// no terminal to be asked on, could never take one.
//
// A sidecar rather than more frontmatter: it records every file, not only
// SKILL.md, and it keeps a list of hashes out of the one block an agent reads
// before anything else. A dotfile, so that it is not mistaken for part of the
// skill by anything listing the directory. It is fft's: never a stray, never a
// row in the plan, and not evidence that a directory is somebody else's.
const ManifestName = ".fft-skill.json"

// manifest is what one install wrote.
//
// Files holds the [digest] of each file's bytes exactly as written — SKILL.md
// stamped, as it is on disk. Hashing what is on disk, rather than the text before
// stamping, keeps the question one comparison with no special case: is this file
// byte for byte what fft put there?
//
// It is not quite the last word for SKILL.md. When the record does not vouch for
// it, [compare] falls back on the stamp, and a SKILL.md that differs from what fft
// ships only in a well-formed version line is updated without asking — so a user
// who edits nothing but that line has the edit quietly put back. That is the
// price of upgrading an install whose record was lost, and a cheap one: the line
// is fft's own bookkeeping, no agent reads instructions in it, and any other edit
// anywhere in the file still stops the install and asks.
type manifest struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

// digest is how a file's bytes are named in a manifest and in [legacy].
func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// readManifest is the record in dir, or an empty one.
//
// Absent, unreadable or malformed all read as "nothing recorded", which fails
// closed: a file fft cannot vouch for is the user's, and installing asks before
// touching it — exactly what happened before there were manifests at all.
func readManifest(dir string) manifest {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return manifest{}
	}

	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return manifest{}
	}
	return m
}

// ours reports whether data is a text fft wrote to name: the one this install
// recorded, or one a release shipped before installs recorded anything.
func (m manifest) ours(name string, data []byte) bool {
	d := digest(data)
	return m.Files[name] == d || slices.Contains(legacy[name], d)
}

// shippedManifest is the record of installing this binary's skill, encoded as it
// is written.
func shippedManifest() ([]byte, error) {
	m := manifest{Version: buildinfo.Version, Files: map[string]string{}}

	err := fs.WalkDir(tree, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := content(name)
		if err != nil {
			return err
		}
		m.Files[name] = digest(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("hash the embedded skill: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// writeManifest records what Apply has just installed, unless the record already
// says exactly that. A reinstall that changed nothing must not churn the mtime of
// a file in the directory any more than of the skill itself.
func writeManifest(dir string) error {
	want, err := shippedManifest()
	if err != nil {
		return err
	}

	target := filepath.Join(dir, ManifestName)
	if got, err := os.ReadFile(target); err == nil && bytes.Equal(got, want) {
		return nil
	}
	return atomicfile.WriteMode(target, want, fileMode, dirMode)
}
