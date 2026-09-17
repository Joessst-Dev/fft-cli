//go:build unix

package history

import "golang.org/x/sys/unix"

// openNonblock keeps an open of a FIFO from waiting for its other end. It changes
// nothing for a regular file, whose reads and writes never wait on it.
const openNonblock = unix.O_NONBLOCK
