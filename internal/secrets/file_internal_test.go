package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fft asks one store to forget a project even when the other store is the one
// holding it, so a delete that finds nothing must be a no-op on disk. Creating an
// empty cleartext credentials file on a machine that has a working keychain is a
// surprising thing for a removal to do, and a confusing thing to find later.
var _ = Describe("deleting from the file store what is not in it", func() {
	It("does not bring the file into existence", func() {
		path := filepath.Join(GinkgoT().TempDir(), "state", "fft", "credentials.json")

		Expect(NewFile(path).Delete("fft:staging:password")).To(Succeed())

		Expect(path).NotTo(BeAnExistingFile())
		Expect(filepath.Dir(path)).NotTo(BeAnExistingFile())
	})

	It("leaves a file that does exist exactly as it was", func() {
		path := filepath.Join(GinkgoT().TempDir(), "credentials.json")
		Expect(os.WriteFile(path, []byte(`{"fft:staging:password":"s3cret"}`), 0o600)).To(Succeed())
		before, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())

		Expect(NewFile(path).Delete("fft:other:password")).To(Succeed())

		after, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		// Not rewritten: same bytes, and the same file rather than a fresh one swapped
		// in by the atomic writer.
		Expect(after.ModTime()).To(Equal(before.ModTime()))
		Expect(os.ReadFile(path)).To(Equal([]byte(`{"fft:staging:password":"s3cret"}`)))
	})

	It("still removes a secret that is there", func() {
		path := filepath.Join(GinkgoT().TempDir(), "credentials.json")
		store := NewFile(path)
		Expect(store.Set("fft:staging:password", "s3cret")).To(Succeed())

		Expect(store.Delete("fft:staging:password")).To(Succeed())

		_, err := store.Get("fft:staging:password")
		Expect(err).To(MatchError(ErrNotFound))
	})
})

var _ = Describe("the file store's loose-permission warning", func() {
	BeforeEach(func() {
		if runtime.GOOS == "windows" {
			Skip("POSIX mode bits do not apply on Windows")
		}
	})

	It("warns once, suggesting chmod 600, for a world-readable file", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "credentials.json")
		Expect(os.WriteFile(path, []byte(`{"fft:staging:password":"s3cret"}`), 0o644)).To(Succeed())

		var warnings []string
		s := &fileStore{path: path, warn: func(m string) { warnings = append(warnings, m) }}

		_, err := s.Get("fft:staging:password")
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).NotTo(BeEmpty(), "a 0644 credentials file drew no warning")
		// The file remediation is chmod 600 — never a directory-only chmod.
		Expect(warnings[0]).To(ContainSubstring("chmod 600"))

		// A read-mostly store must not repeat the warning on every read.
		before := len(warnings)
		_, err = s.Get("fft:staging:password")
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(HaveLen(before), "warned again on the second read")
	})

	It("warns before the first write when the directory is world-reachable, suggesting chmod 700", func() {
		// The credentials file does not exist yet, so the warning has to fire before
		// load reads a file that is not there — otherwise `fft project add` would drop
		// the cleartext token into a 0777 dir in silence.
		dir := GinkgoT().TempDir()
		Expect(os.Chmod(dir, 0o777)).To(Succeed())
		path := filepath.Join(dir, "credentials.json") // does not exist yet

		var warnings []string
		s := &fileStore{path: path, warn: func(m string) { warnings = append(warnings, m) }}

		// A missing file is a clean miss (ErrNotFound), but load still runs the
		// permission check on the directory that is about to receive the token.
		_, err := s.Get("fft:staging:password")
		Expect(err == nil || errors.Is(err, ErrNotFound)).To(BeTrue())
		Expect(warnings).NotTo(BeEmpty(), "a world-reachable directory drew no warning")
		// A directory needs its traverse bit, so the fix is chmod 700, never 600.
		Expect(warnings[0]).To(ContainSubstring("chmod 700"))
	})

	It("stays quiet for a 0600 file in a 0700 directory", func() {
		dir := GinkgoT().TempDir()
		Expect(os.Chmod(dir, 0o700)).To(Succeed())
		path := filepath.Join(dir, "credentials.json")
		Expect(os.WriteFile(path, []byte(`{"fft:staging:password":"s3cret"}`), 0o600)).To(Succeed())

		var warnings []string
		s := &fileStore{path: path, warn: func(m string) { warnings = append(warnings, m) }}

		_, err := s.Get("fft:staging:password")
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeEmpty())
	})
})
