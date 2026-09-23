//go:build windows

package history

import (
	"errors"
	"fmt"
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
// while any process holds the file open the rename is.
//
// An open waits out one rename and gets its turn. A rename may never get one:
// fft processes appending in a loop leave no gap between their handles, so the
// whole budget can pass with the file never once free. No budget always outlasts
// that, so running out of it is reported as errBusy rather than as a failure, and
// the caller decides what it is worth.
const shareWait = 250 * time.Millisecond

// openFile opens the history file, waiting out a rename in flight. The one error
// it waits on is the sharing violation, which is contention by definition; access
// denied is left alone, because from an open it means what it says.
func openFile(path string, flag int) (*os.File, error) {
	var f *os.File
	busy, err := retryShared(func() error {
		var err error
		f, err = os.OpenFile(path, flag, atomicfile.FileMode) //nolint:gosec // the history path, the user's own file
		return err
	}, windows.ERROR_SHARING_VIOLATION)
	if busy {
		return nil, fmt.Errorf("%w: %w", errBusy, err)
	}
	return f, err
}

// replace is atomicfile.Write, retried while an open handle refuses the rename.
// Such a refusal reads as access denied, which is why that error is retried here
// and nowhere else: from an open it means what it says.
//
// Only the rename is taken for busy. atomicfile.Write creates a temporary file
// first, and a state directory that refuses that answers with the same access
// denied — a standing "no" that has to keep reading as a failure rather than as a
// turn to skip. A directory can refuse it while the history file itself stays
// appendable, so the append that got this far proves nothing about it. os.Rename
// is the one step of the write that reports an *os.LinkError, which is how the two
// are told apart.
//
// That separates the steps, not the temporary from the permanent: Windows does not
// distinguish them here, and an ACL that lets a file be created but not replaced
// refuses the rename with the same access denied a held handle does. It reads as
// busy for as long as it stands, and compaction then stops happening and says
// nothing. Nothing else changes — no command fails, and the file grows exactly as
// it did when this was reported instead — which is why it is left rather than paid
// for by probing the target on a path every append takes.
func replace(path string, data []byte) error {
	busy, err := retryShared(func() error {
		return atomicfile.Write(path, data)
	}, windows.ERROR_SHARING_VIOLATION, windows.ERROR_ACCESS_DENIED)

	var link *os.LinkError
	if busy && errors.As(err, &link) {
		return fmt.Errorf("%w: %w", errBusy, err)
	}
	return err
}

// tightenDir and tightenFile do nothing on Windows, where access is an ACL the
// user profile already confines and a mode is only a read-only bit.
func tightenDir(string) error { return nil }

func tightenFile(*os.File, fs.FileInfo) error { return nil }

// retryShared runs op until it succeeds or fails for a reason other than one of
// transient, and reports busy when shareWait passed with op still blocked by one.
// Whether being blocked is a failure is the caller's to decide, so the error is
// handed back as it came and the judgement is left there.
func retryShared(op func() error, transient ...error) (busy bool, err error) {
	deadline := time.Now().Add(shareWait)
	for {
		err = op()
		if err == nil || !isAny(err, transient) {
			return false, err
		}
		if time.Now().After(deadline) {
			return true, err
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
