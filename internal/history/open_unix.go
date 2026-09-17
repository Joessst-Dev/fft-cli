//go:build unix

package history

import (
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
