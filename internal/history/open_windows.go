//go:build windows

package history

// openNonblock is zero on Windows: a named pipe lives under \\.\pipe\, not at a
// path in the state directory, so an open there never waits on a peer.
const openNonblock = 0
