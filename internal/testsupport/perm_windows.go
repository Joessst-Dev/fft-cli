//go:build windows

package testsupport

import (
	"io/fs"
	"os/exec"
	"os/user"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

// The assertion below is skipped rather than deleted, and skipped rather than
// weakened into something that would pass, so that a Windows CI run says out
// loud which security guarantee it did not check.
const permSkip = "Windows has no POSIX mode bits: file security is an ACL, os.Stat " +
	"synthesises 0666 for every regular file, and fft sets no ACL of its own — " +
	"the file is protected only by what it inherits from its parent directory. " +
	"README, 'On Windows, --no-keyring protects less than 0600 suggests', has " +
	"the consequences."

func expectPerm(_ string, _ fs.FileMode) {
	ginkgo.GinkgoHelper()
	ginkgo.Skip(permSkip)
}

// makeUnwritable denies the account running the suite the right to create files
// in dir.
//
// This one does not skip. "A failed save leaves the previous file intact" is
// atomicfile's contract, not a Unix one, and Windows — where renaming over an
// existing file is least like POSIX — is the platform where it most wants
// pinning. os.Chmod was only ever the wrong tool for the job: on Windows it
// toggles the read-only attribute, which a directory ignores. Security here is
// an ACL, so the ACL is what has to change.
//
// The principal is the account's own SID. A deny ACE for it beats every allow
// ACE the directory inherits, including the ones the account holds as a member
// of Administrators — which is what CI runs as — because the access check reads
// deny entries first. A SID also needs no domain qualifier and survives a
// Windows whose well-known names are localised; user.Current reports it
// verbatim, and icacls wants it prefixed with '*'.
func makeUnwritable(dir string) {
	ginkgo.GinkgoHelper()

	me, err := user.Current()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	sid := "*" + me.Uid

	// (W) is the generic write right, and on a directory that carries
	// FILE_ADD_FILE. Denying it stops atomicfile's temporary file from being
	// created at all, so the rename that would replace the target never runs.
	// It leaves DELETE alone, so the suite's temp-directory teardown can still
	// remove dir even in the case where the cleanup below never runs.
	icacls(dir, "/deny", sid+":(W)")
	ginkgo.DeferCleanup(func() {
		// The exact inverse: /remove:d drops this principal's deny entries and
		// touches nothing else. Ginkgo runs cleanup even when the spec failed,
		// and registration order puts this before the temp directory's own
		// teardown — which it has to be, or that teardown would fail and the
		// failure would surface on whichever spec ran next.
		icacls(dir, "/remove:d", sid)
	})
}

// icacls runs the Windows ACL editor over dir and fails the spec with its output
// if it does not succeed. The output is the whole diagnosis: icacls reports
// "Failed processing 1 files" on its stdout and merely exits non-zero.
func icacls(dir string, args ...string) {
	ginkgo.GinkgoHelper()

	cmd := exec.Command("icacls", append([]string{dir}, args...)...)
	out, err := cmd.CombinedOutput()
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "%s\n%s", strings.Join(cmd.Args, " "), out)
}
