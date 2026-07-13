// Package atomicfile writes private files without ever leaving a half-written
// one behind.
//
// Both the config file and the fallback credentials file are rewritten in full
// on every change. A crash or a full disk part-way through a plain os.WriteFile
// would truncate the user's projects; writing to a temporary file in the same
// directory and renaming it over the target makes the swap atomic, so a reader
// sees either the old file or the new one.
package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// File and directory modes. Both files hold credentials or credential-adjacent
// data, so they are owner-only.
const (
	FileMode = 0o600
	DirMode  = 0o700
)

// Write creates path's parent directory if needed and replaces path with data,
// atomically. The file ends up mode 0600 and the directory mode 0700.
func Write(path string, data []byte) error {
	return WriteMode(path, data, FileMode, DirMode)
}

// WriteMode is [Write] with the modes spelled out, for the files that are not
// secrets.
//
// The installed agent skill is documentation: it belongs to the user, other
// tools may want to read it, and 0600 would be a claim about it that is not
// true. What it does want is the rest of Write — a SKILL.md truncated by a full
// disk is a skill that lies to an agent, which is worse than one that is absent.
func WriteMode(path string, data []byte, file, dir os.FileMode) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, dir); err != nil {
		return fmt.Errorf("create %s: %w", parent, err)
	}

	// The temporary file must share a filesystem with the target, or the rename
	// below degrades into a copy and stops being atomic. Same directory, then.
	tmp, err := os.CreateTemp(parent, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", parent, err)
	}
	tmpName := tmp.Name()

	// Everything from here on can fail, and every failure must take the
	// temporary file with it. After a successful rename tmpName no longer
	// exists, so the remove becomes a no-op.
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	// os.CreateTemp always uses 0600, so this is the line — and the only line —
	// that decides what the file ends up as. For a credential it keeps the file
	// private and must not depend on a default; for a skill it is what makes the
	// file readable at all.
	if err := tmp.Chmod(file); err != nil {
		return fmt.Errorf("set the mode of %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	// Flush to disk before the rename, so a crash cannot leave the directory
	// entry pointing at a file whose contents never made it out of the cache.
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("flush %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// ErrNotPrivate reports a file that others can read. It is advisory: fft warns
// rather than refuses, because the user may have good reasons and a CLI that
// bricks itself over a permission bit is worse than one that complains.
var ErrNotPrivate = errors.New("file is readable by users other than its owner")

// CheckPrivate reports ErrNotPrivate if path is group- or world-accessible. A
// missing file is fine — there is nothing to leak.
func CheckPrivate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%s has mode %#o: %w", path, perm, ErrNotPrivate)
	}
	return nil
}
