//go:build unix

package history

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
)

// openFlags are added to every open of the history file.
//
// O_NONBLOCK keeps an open of a FIFO from waiting for its other end; it changes
// nothing for a regular file, whose reads and writes never wait on it. O_NOFOLLOW
// refuses a symlink planted at the path, which would otherwise have fft append to,
// and compaction rewrite, whatever file it points at.
const openFlags = unix.O_NONBLOCK | unix.O_NOFOLLOW

func openFile(path string, flag int) (*os.File, error) {
	return os.OpenFile(path, flag, atomicfile.FileMode) //nolint:gosec // the history path, the user's own file
}

// replace swaps the compacted file into place. A rename over a file another
// process holds open is fine here, unlike on Windows.
func replace(path string, data []byte) error {
	return atomicfile.Write(path, data)
}

// private is the permission bits a history file or directory must not have: the
// entries name projects, operations and the values typed on a command line.
const private fs.FileMode = 0o077

// tightenDir takes group and other access away from the history directory. It
// was only created 0700 if fft created it; one another tool made first keeps
// whatever that tool chose.
func tightenDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if perm := info.Mode().Perm(); perm&private != 0 {
		return os.Chmod(dir, perm&^private)
	}
	return nil
}

// tightenFile does the same for the open history file, which a restore or a
// touch may have left readable by others.
func tightenFile(f *os.File, info fs.FileInfo) error {
	if perm := info.Mode().Perm(); perm&private != 0 {
		return f.Chmod(perm &^ private)
	}
	return nil
}
