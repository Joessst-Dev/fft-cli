//go:build windows

package history

import (
	"errors"
	"io/fs"
	"os"
	"time"

	"golang.org/x/sys/windows"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
)

// openFlags is zero on Windows: a named pipe lives under \\.\pipe\, not at a
// path in the state directory, so an open there never waits on a peer. Windows has
// no O_NOFOLLOW; a link at the path is refused by the Lstat that precedes the open.
const openFlags = 0

// shareWait bounds how long an open or a compaction waits out another fft's
// handle on the file. Go opens files without FILE_SHARE_DELETE, so while one
// process renames a compacted file into place every other open is refused, and
// while any process holds the file open the rename is. Either side holds it for
// one small write or read, so the conflict clears long before this.
const shareWait = 250 * time.Millisecond

func openFile(path string, flag int) (*os.File, error) {
	var f *os.File
	err := retryShared(func() error {
		var err error
		f, err = os.OpenFile(path, flag, atomicfile.FileMode) //nolint:gosec // the history path, the user's own file
		return err
	}, windows.ERROR_SHARING_VIOLATION)
	return f, err
}

// replace is atomicfile.Write, retried while an open handle refuses the rename.
// Such a refusal reads as access denied, which is why that error is retried here
// and nowhere else: from an open it means what it says.
func replace(path string, data []byte) error {
	return retryShared(func() error {
		return atomicfile.Write(path, data)
	}, windows.ERROR_SHARING_VIOLATION, windows.ERROR_ACCESS_DENIED)
}

// tightenDir and tightenFile do nothing on Windows, where access is an ACL the
// user profile already confines and a mode is only a read-only bit.
func tightenDir(string) error { return nil }

func tightenFile(*os.File, fs.FileInfo) error { return nil }

func retryShared(op func() error, transient ...error) error {
	deadline := time.Now().Add(shareWait)
	for {
		err := op()
		if err == nil || time.Now().After(deadline) || !isAny(err, transient) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}

func isAny(err error, targets []error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
