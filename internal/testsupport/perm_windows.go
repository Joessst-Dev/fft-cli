//go:build windows

package testsupport

import (
	"io/fs"

	"github.com/onsi/ginkgo/v2"
)

// The assertions below are skipped rather than deleted, and skipped rather than
// weakened into something that would pass, so that a Windows CI run says out
// loud which security guarantee it did not check.
const (
	permSkip = "Windows has no POSIX mode bits: file security is an ACL, os.Stat " +
		"synthesises 0666 for every regular file, and fft sets no ACL of its own — " +
		"the file is protected only by what it inherits from its parent directory. " +
		"README, 'On Windows, --no-keyring protects less than 0600 suggests', has " +
		"the consequences."

	chmodSkip = "Windows os.Chmod only toggles the read-only attribute and cannot " +
		"make a directory unwritable, so the write this spec expects to fail would " +
		"succeed instead."
)

func expectPerm(_ string, _ fs.FileMode) {
	ginkgo.GinkgoHelper()
	ginkgo.Skip(permSkip)
}

func makeUnwritable(_ string) {
	ginkgo.GinkgoHelper()
	ginkgo.Skip(chmodSkip)
}
