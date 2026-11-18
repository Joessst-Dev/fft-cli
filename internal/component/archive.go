package component

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// unpack writes an archive's contents into dir.
//
// Both formats GoReleaser produces are supported on every platform, chosen by the
// asset's name rather than by GOOS: a user who downloaded the wrong archive should
// be told that it is the wrong archive, not that the file is corrupt.
func unpack(name string, archive []byte, dir string) error {
	var extract func([]byte, *os.Root) error
	switch {
	case strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz"):
		extract = unpackTarGz
	case strings.HasSuffix(name, ".zip"):
		extract = unpackZip
	default:
		return fmt.Errorf("%s is neither a .tar.gz nor a .zip", name)
	}

	root, err := openRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return extract(archive, root)
}

// openRoot opens the directory an archive is unpacked into as an os.Root, so that
// containment is the kernel's to enforce rather than a property of the path strings
// alone: a write that would resolve outside dir — through "..", or through a symlink
// that is already there — fails, whatever localPath made of its name.
func openRoot(dir string) (*os.Root, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}
	return root, nil
}

func unpackTarGz(archive []byte, root *os.Root) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("read the archive: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	budget := &writeBudget{}
	for files := 0; ; files++ {
		if files > maxFiles {
			return fmt.Errorf("the archive holds more than %d files", maxFiles)
		}

		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the archive: %w", err)
		}

		switch header.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			// Not a file: a pax extended-header record, which git and some tar tools
			// prepend. It carries no path of its own to unpack — skip it, rather than
			// reject the whole archive as "not a regular file".
			continue
		}

		name, err := localPath(header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := mkdirAll(root, name); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(root, name, tr, modeFor(header.FileInfo().Mode()), budget); err != nil {
				return err
			}
		default:
			// Symlinks especially: a link is a second way to name a file, and every
			// argument localPath makes about paths would have to be made again about where
			// the link points. A component has no need of one.
			return fmt.Errorf("the archive contains %q, which is not a regular file or a directory", header.Name)
		}
	}
}

func unpackZip(archive []byte, root *os.Root) error {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return fmt.Errorf("read the archive: %w", err)
	}
	if len(zr.File) > maxFiles {
		return fmt.Errorf("the archive holds more than %d files", maxFiles)
	}

	budget := &writeBudget{}
	for _, entry := range zr.File {
		name, err := localPath(entry.Name)
		if err != nil {
			return err
		}

		if entry.FileInfo().IsDir() {
			if err := mkdirAll(root, name); err != nil {
				return err
			}
			continue
		}
		if !entry.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("the archive contains %q, which is not a regular file or a directory", entry.Name)
		}

		rc, err := entry.Open()
		if err != nil {
			return fmt.Errorf("read %s from the archive: %w", entry.Name, err)
		}
		err = writeFile(root, name, rc, modeFor(entry.FileInfo().Mode()), budget)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// modeFor normalises an archive entry's mode to one of two: executable or not.
//
// What a tarball records is whoever built it's umask, and reproducing it faithfully
// is how a component arrives group-writable. The one bit that carries meaning is
// whether the file is meant to be run.
func modeFor(mode fs.FileMode) fs.FileMode {
	if mode&0o111 != 0 {
		return execMode
	}
	return fileMode
}

// writeBudget caps the total bytes an archive may unpack to.
//
// The per-file cap alone bounds one file; this bounds the sum, so a small archive of
// many near-cap files cannot fill the disk. It is defence in depth behind the
// checksum — an archive that reaches this point already matched the digest the
// release published — but a running total costs nothing and closes the gap for a
// hostile *release*, which the checksum cannot.
type writeBudget struct{ written int64 }

// maxTotal is the ceiling on an unpacked component: generous for a static binary and
// its manifest, and far below a disk-filling bomb.
const maxTotal = 512 << 20

func (b *writeBudget) add(n int64) error {
	b.written += n
	if b.written > maxTotal {
		return fmt.Errorf("the archive unpacks to more than %d bytes", maxTotal)
	}
	return nil
}

// mkdirAll creates a directory, and its parents, inside root.
func mkdirAll(root *os.Root, name string) error {
	if err := root.MkdirAll(name, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Join(root.Name(), name), err)
	}
	return nil
}

// writeFile creates one file of an unpacked archive, capped per file and against the
// archive's running total, so that a small archive cannot decompress into a large
// disk.
func writeFile(root *os.Root, name string, r io.Reader, mode fs.FileMode, budget *writeBudget) error {
	if err := mkdirAll(root, filepath.Dir(name)); err != nil {
		return err
	}

	target := filepath.Join(root.Name(), name)
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	defer func() { _ = f.Close() }()

	written, err := io.Copy(f, io.LimitReader(r, maxFile+1))
	if err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	if written > maxFile {
		return fmt.Errorf("write %s: larger than %d bytes", target, maxFile)
	}
	if err := budget.add(written); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return f.Close()
}

// copyTree copies a local component directory into the staging directory, which is
// how `fft component install --path` works.
//
// It is a copy rather than a symlink on purpose: a component that lives wherever it
// was built is a component that changes, or vanishes, without fft knowing. An
// installed one is a snapshot.
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory: --path takes an unpacked component", src)
	}

	root, err := openRoot(dst)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	files := 0
	budget := &writeBudget{}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		name, err := localPath(filepath.ToSlash(rel))
		if err != nil {
			return err
		}

		if d.IsDir() {
			return mkdirAll(root, name)
		}
		if files++; files > maxFiles {
			return fmt.Errorf("%s holds more than %d files", src, maxFiles)
		}

		entry, err := d.Info()
		if err != nil {
			return err
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}

		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		return writeFile(root, name, f, modeFor(entry.Mode()), budget)
	})
}

// marshalManifest renders a manifest back to YAML, which is what Commit writes so
// that the installed copy records where it came from.
func marshalManifest(m Manifest) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(m); err != nil {
		return nil, fmt.Errorf("render %s: %w", ManifestName, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("render %s: %w", ManifestName, err)
	}
	return buf.Bytes(), nil
}
