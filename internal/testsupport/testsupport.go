// Package testsupport holds Ginkgo helpers shared by the suites of several
// packages. Nothing outside a _test.go file imports it.
//
// It exists for assertions that more than one suite needs and that mean
// something different per platform. Two things live here today, and they are
// unlike each other:
//
// The mode assertions. fft writes its config, its credentials fallback and its
// update cache owner-only; four suites check that; and on Windows none of those
// checks can hold, because Windows has no POSIX mode bits. A file's security
// there is an ACL and os.Stat reports a synthetic 0666 for every regular file,
// so there is nothing to assert. ExpectOwnerOnly* therefore skip the calling
// spec on Windows, loudly and with a reason, rather than assert a guarantee the
// platform does not make. What that means for fft's secrets on Windows is
// written down in the README, under the keychain section — a skipped security
// assertion must not be the only record that the guarantee is weaker.
//
// Making a directory unwritable. This one is not a mode-bits problem and does
// not skip anywhere. The spec it serves pins atomicfile's contract — a save that
// cannot complete leaves the previous file intact — which every platform is
// meant to keep. Windows only needs a different lever to pull: a deny ACE
// instead of a mode bit.
package testsupport

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

// Modes fft writes its private files and their directories with. They mirror
// atomicfile.FileMode and atomicfile.DirMode; they are repeated rather than
// imported so that a spec asserts an expectation of its own, and a regression
// in atomicfile cannot rewrite the expectation along with the code.
const (
	fileMode fs.FileMode = 0o600
	dirMode  fs.FileMode = 0o700
)

// Modes fft writes the installed agent skill with. It is documentation, not a
// secret, and the user's editor and their agent both have to be able to read it —
// so here the assertion is the opposite one, and it is worth making for the same
// reason: a skill written 0600 would be a skill that silently does not load.
const (
	readableFileMode fs.FileMode = 0o644
	readableDirMode  fs.FileMode = 0o755
)

// ExpectReadableFile asserts that path is a file anyone may read — mode 0644. On
// Windows it skips the calling spec; see the package doc.
func ExpectReadableFile(path string) {
	ginkgo.GinkgoHelper()
	expectPerm(path, readableFileMode)
}

// ExpectReadableDir asserts that path is a directory anyone may enter — mode
// 0755. On Windows it skips the calling spec; see the package doc.
func ExpectReadableDir(path string) {
	ginkgo.GinkgoHelper()
	expectPerm(path, readableDirMode)
}

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

// MakeUnwritableDir takes away the right to create files in dir for the rest of
// the spec, so that a spec can watch the code under test fail to write there.
// The right is restored on cleanup, or the suite's own temp-directory teardown
// would be unable to remove dir.
//
// How the right is taken away differs per platform — a mode bit off on Unix, a
// deny ACE on Windows — so this then proves it worked, by trying the very thing
// the code under test is about to try. A platform where the lever quietly did
// nothing would still fail the calling spec, because the save it expects to fail
// would succeed; but it would fail it as though atomicfile had regressed, which
// is the wrong culprit and a long afternoon. A helper whose whole purpose is to
// make a write fail should be the one to say when it could not.
func MakeUnwritableDir(dir string) {
	ginkgo.GinkgoHelper()

	makeUnwritable(dir)
	expectNoFileCanBeCreatedIn(dir)
}

func expectNoFileCanBeCreatedIn(dir string) {
	ginkgo.GinkgoHelper()

	f, err := os.CreateTemp(dir, ".probe-*")
	if err == nil {
		name := f.Name()
		gomega.Expect(f.Close()).To(gomega.Succeed())
		gomega.Expect(os.Remove(name)).To(gomega.Succeed())
		ginkgo.Fail(fmt.Sprintf(
			"%s was supposed to be unwritable, but creating %s in it succeeded — "+
				"the spec that called this would be asserting a failure that never happens",
			dir, filepath.Base(name)))
	}
	gomega.Expect(err).To(gomega.MatchError(fs.ErrPermission))
}
