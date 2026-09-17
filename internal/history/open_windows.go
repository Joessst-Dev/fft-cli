//go:build windows

package history

// openFlags is zero on Windows: a named pipe lives under \\.\pipe\, not at a
// path in the state directory, so an open there never waits on a peer. Windows has
// no O_NOFOLLOW; a link at the path is refused by the Lstat that precedes the open.
const openFlags = 0
