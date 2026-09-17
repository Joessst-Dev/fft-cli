//go:build unix

package history

import "golang.org/x/sys/unix"

// openFlags are added to every open of the history file.
//
// O_NONBLOCK keeps an open of a FIFO from waiting for its other end; it changes
// nothing for a regular file, whose reads and writes never wait on it. O_NOFOLLOW
// refuses a symlink planted at the path, which would otherwise have fft append to,
// and compaction rewrite, whatever file it points at.
const openFlags = unix.O_NONBLOCK | unix.O_NOFOLLOW
