//go:build unix

package history

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
)

// tryLock takes an exclusive advisory lock on path, trying until wait has passed.
// It reports false, and no error, when another process kept the lock throughout.
func tryLock(path string, wait time.Duration) (unlock func(), locked bool, err error) {
	// O_NOFOLLOW, so that a symlink planted at the lock's path cannot have fft
	// create, or lock, a file elsewhere. os.OpenFile adds O_CLOEXEC itself.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, atomicfile.FileMode)
	if errors.Is(err, unix.ELOOP) {
		return nil, false, fmt.Errorf("%s: %w", path, errNotRegular)
	}
	if err != nil {
		return nil, false, err
	}

	deadline := time.Now().Add(wait)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				// Closing the descriptor releases the lock whatever Flock would say.
				_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
				_ = f.Close()
			}, true, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) || time.Now().After(deadline) {
			closeErr := f.Close()
			if errors.Is(err, unix.EWOULDBLOCK) {
				return nil, false, closeErr
			}
			return nil, false, errors.Join(fmt.Errorf("flock: %w", err), closeErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
