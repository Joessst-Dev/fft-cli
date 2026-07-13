// Package testsupport holds Ginkgo helpers shared by the suites of several
// packages. Nothing outside a _test.go file imports it.
//
// It exists for assertions that more than one suite needs and that mean
// something different per platform. The file-permission assertions are the
// whole of it today: fft writes its config, its credentials fallback and its
// update cache owner-only; four suites check that; and on Windows none of those
// checks can hold, because Windows has no POSIX mode bits. A file's security
// there is an ACL, os.Stat reports a synthetic 0666 for every regular file, and
// os.Chmod only toggles the read-only attribute. So on Windows these helpers
// skip the calling spec, loudly and with a reason, rather than assert a
// guarantee the platform does not make.
//
// What that means for fft's secrets on Windows is written down in the README,
// under the keychain section — a skipped security assertion must not be the
// only record that the guarantee is weaker.
package testsupport

import (
	"io/fs"

	"github.com/onsi/ginkgo/v2"
)

// Modes fft writes its private files and their directories with. They mirror
// atomicfile.FileMode and atomicfile.DirMode; they are repeated rather than
// imported so that a spec asserts an expectation of its own, and a regression
// in atomicfile cannot rewrite the expectation along with the code.
const (
	fileMode fs.FileMode = 0o600
	dirMode  fs.FileMode = 0o700
)

// ExpectOwnerOnlyFile asserts that path is a file no user but its owner can
// read — mode 0600. On Windows it skips the calling spec; see the package doc.
func ExpectOwnerOnlyFile(path string) {
	ginkgo.GinkgoHelper()
	expectPerm(path, fileMode)
}

// ExpectOwnerOnlyDir asserts that path is a directory no user but its owner can
// enter — mode 0700. On Windows it skips the calling spec; see the package doc.
func ExpectOwnerOnlyDir(path string) {
	ginkgo.GinkgoHelper()
	expectPerm(path, dirMode)
}

// MakeUnwritableDir takes the write bit off dir for the rest of the spec, so
// that a spec can watch the code under test fail to create a file there. The
// mode is restored on cleanup, or the suite's own temp-directory teardown would
// be unable to remove it.
//
// On Windows it skips the calling spec: os.Chmod cannot make a directory
// unwritable there, so the write under test would succeed and the spec would be
// asserting a failure that never happened.
func MakeUnwritableDir(dir string) {
	ginkgo.GinkgoHelper()
	makeUnwritable(dir)
}
