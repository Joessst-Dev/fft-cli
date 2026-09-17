//go:build windows

package history

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
)

// tryLock takes an exclusive lock on path, trying until wait has passed. It
// reports false, and no error, when another process kept the lock throughout.
func tryLock(path string, wait time.Duration) (unlock func(), locked bool, err error) {
	// Windows has no O_NOFOLLOW. Looking first narrows, but cannot close, the
	// window in which a link planted at the path would have fft lock a file
	// elsewhere.
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%s: %w", path, errNotRegular)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, atomicfile.FileMode)
	if err != nil {
		return nil, false, err
	}
	handle := windows.Handle(f.Fd())

	deadline := time.Now().Add(wait)
	for {
		ol := new(windows.Overlapped)
		err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
		if err == nil {
			return func() {
				// Closing the handle releases the lock whatever UnlockFileEx would say.
				_ = windows.UnlockFileEx(handle, 0, 1, 0, ol)
				_ = f.Close()
			}, true, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) || time.Now().After(deadline) {
			closeErr := f.Close()
			if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
				return nil, false, closeErr
			}
			return nil, false, errors.Join(fmt.Errorf("LockFileEx: %w", err), closeErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
