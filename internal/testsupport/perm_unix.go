//go:build !windows

package testsupport

import (
	"io/fs"
	"os"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func expectPerm(path string, want fs.FileMode) {
	ginkgo.GinkgoHelper()

	info, err := os.Stat(path)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(info.Mode().Perm()).To(gomega.Equal(want))
}

// ownerWrite is the only bit that comes off dir: the directory stays readable
// and searchable, it just cannot be written to. Taking it off dirMode rather
// than writing 0500 says which of the two modes is the derived one.
const ownerWrite fs.FileMode = 0o200

func makeUnwritable(dir string) {
	ginkgo.GinkgoHelper()

	gomega.Expect(os.Chmod(dir, dirMode&^ownerWrite)).To(gomega.Succeed())
	ginkgo.DeferCleanup(func() {
		gomega.Expect(os.Chmod(dir, dirMode)).To(gomega.Succeed())
	})
}
