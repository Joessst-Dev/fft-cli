//go:build !windows

package main

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// breakComponentRoot forces a real, non-fs.ErrNotExist failure out of a
// following os.ReadDir(root): replacing the directory with a regular file
// makes ReadDir fail with ENOTDIR. That holds regardless of privilege — unlike
// a permission bit, which a suite running as root would simply ignore.
func breakComponentRoot(root string) {
	GinkgoHelper()

	Expect(os.RemoveAll(root)).To(Succeed())
	Expect(os.WriteFile(root, []byte("not a directory"), 0o600)).To(Succeed())
}
