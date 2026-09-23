//go:build windows

package main

import "github.com/Joessst-Dev/fft-cli/internal/testsupport"

// breakComponentRoot forces a real, non-fs.ErrNotExist failure out of a
// following os.ReadDir(root). Replacing root with a regular file, the
// technique the other platforms use, does not do that here: os.ReadDir on a
// path that used to be a directory and is now a plain file reports the same
// shape Windows reports for a path that was never there, so it would exercise
// the missing-root path instead of the real-failure one this spec means to
// test. Denying list access to the directory itself keeps it a real,
// existing root that genuinely cannot be read.
func breakComponentRoot(root string) {
	testsupport.MakeUnreadableDir(root)
}
