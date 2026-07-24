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
	switch {
	case strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz"):
		return unpackTarGz(archive, dir)
	case strings.HasSuffix(name, ".zip"):
		return unpackZip(archive, dir)
	default:
		return fmt.Errorf("%s is neither a .tar.gz nor a .zip", name)
	}
}

func unpackTarGz(archive []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("read the archive: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
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

		target, err := safeJoin(dir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, dirMode); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := writeFile(target, tr, modeFor(header.FileInfo().Mode())); err != nil {
				return err
			}
		default:
			// Symlinks especially: a link is a second way to name a file, and every
			// argument safeJoin makes about paths would have to be made again about where
			// the link points. A component has no need of one.
			return fmt.Errorf("the archive contains %q, which is not a regular file or a directory", header.Name)
		}
	}
}

func unpackZip(archive []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return fmt.Errorf("read the archive: %w", err)
	}
	if len(zr.File) > maxFiles {
		return fmt.Errorf("the archive holds more than %d files", maxFiles)
	}

	for _, entry := range zr.File {
		target, err := safeJoin(dir, entry.Name)
		if err != nil {
			return err
		}

		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, dirMode); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
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
		err = writeFile(target, rc, modeFor(entry.FileInfo().Mode()))
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

// writeFile creates one file of an unpacked archive, capped so that a small archive
// cannot decompress into a large disk.
func writeFile(target string, r io.Reader, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}

	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
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

	files := 0
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

		target, err := safeJoin(dst, filepath.ToSlash(rel))
		if err != nil {
			return err
		}

		if d.IsDir() {
			return os.MkdirAll(target, dirMode)
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

		return writeFile(target, f, modeFor(entry.Mode()))
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
