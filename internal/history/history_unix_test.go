//go:build unix

package history_test

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"

	"github.com/Joessst-Dev/fft-cli/internal/history"
)

var _ = Describe("a history path that is not a regular file", func() {
	var log history.Log

	BeforeEach(func() {
		dir := GinkgoT().TempDir()
		log = history.Log{Path: filepath.Join(dir, "history.jsonl")}
		Expect(unix.Mkfifo(log.Path, 0o600)).To(Succeed())

		// A call that did block waits on the other end of the pipe. Opening both ends
		// without blocking releases it, so that a failing spec does not leave a
		// goroutine stuck in open(2) for the rest of the suite.
		DeferCleanup(func() {
			for _, flag := range []int{os.O_RDONLY, os.O_WRONLY} {
				if f, err := os.OpenFile(log.Path, flag|unix.O_NONBLOCK, 0); err == nil {
					_ = f.Close()
				}
			}
		})
	})

	// within runs call on a goroutine of its own, and fails the spec if it has not
	// returned within a second.
	within := func(call func() error) error {
		GinkgoHelper()
		done := make(chan error, 1)
		go func() { done <- call() }()
		var err error
		Eventually(done).WithTimeout(time.Second).Should(Receive(&err), "the call is still waiting on the pipe")
		return err
	}

	It("is refused by a read, rather than waited on", func() {
		err := within(func() error {
			_, err := log.Read()
			return err
		})
		Expect(err).To(MatchError(ContainSubstring("not a regular file")))
	})

	It("is refused by an append, rather than waited on", func() {
		err := within(func() error { return log.Append(entry("prod", "getFacility", 1)) })
		Expect(err).To(MatchError(ContainSubstring("not a regular file")))
	})

	It("is not written to by an append, even while something reads the pipe", func() {
		reader, err := os.OpenFile(log.Path, os.O_RDONLY|unix.O_NONBLOCK, 0)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(reader.Close)

		err = within(func() error { return log.Append(entry("prod", "getFacility", 1)) })
		Expect(err).To(MatchError(ContainSubstring("not a regular file")))

		buf := make([]byte, 64)
		n, _ := reader.Read(buf)
		Expect(buf[:n]).To(BeEmpty(), "no entry went down the pipe")
	})

	It("is refused by a clear, which leaves it in place", func() {
		err := within(func() error {
			_, err := log.Clear()
			return err
		})
		Expect(err).To(MatchError(ContainSubstring("not a regular file")))
		Expect(log.Path).To(BeAnExistingFile())
	})
})

var _ = Describe("a history path that is a symlink", func() {
	var (
		log    history.Log
		target string
	)

	BeforeEach(func() {
		dir := GinkgoT().TempDir()
		target = filepath.Join(dir, "elsewhere.txt")
		Expect(os.WriteFile(target, []byte("untouched\n"), 0o600)).To(Succeed())
		log = history.Log{Path: filepath.Join(dir, "history.jsonl")}
		Expect(os.Symlink(target, log.Path)).To(Succeed())
	})

	It("is refused by an append, which leaves the file it points at alone", func() {
		Expect(log.Append(entry("prod", "getFacility", 1))).To(MatchError(ContainSubstring("not a regular file")))
		Expect(os.ReadFile(target)).To(BeEquivalentTo("untouched\n"))
	})

	It("is refused by a read", func() {
		_, err := log.Read()
		Expect(err).To(MatchError(ContainSubstring("not a regular file")))
	})
})

var _ = Describe("a compaction lock path that is a symlink", func() {
	It("is refused, rather than creating the file it points at", func() {
		dir := GinkgoT().TempDir()
		log := history.Log{Path: filepath.Join(dir, "history.jsonl"), MaxBytes: 64}
		target := filepath.Join(dir, "elsewhere.lock")
		Expect(os.Symlink(target, log.Path+".lock")).To(Succeed())

		err := log.Append(entry("prod", "getFacility", 1))

		Expect(err).To(MatchError(ContainSubstring("not a regular file")))
		Expect(target).NotTo(BeAnExistingFile())
	})
})

var _ = Describe("a history that others can read", func() {
	var (
		dir string
		log history.Log
	)

	BeforeEach(func() {
		dir = filepath.Join(GinkgoT().TempDir(), "fft")
		log = history.Log{Path: filepath.Join(dir, "history.jsonl")}
	})

	mode := func(path string) os.FileMode {
		GinkgoHelper()
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		return info.Mode().Perm()
	}

	It("is made private by the next append", func() {
		Expect(os.MkdirAll(dir, 0o700)).To(Succeed())
		Expect(os.WriteFile(log.Path, nil, 0o600)).To(Succeed())
		Expect(os.Chmod(log.Path, 0o644)).To(Succeed())

		Expect(log.Append(entry("prod", "getFacility", 1))).To(Succeed())

		Expect(mode(log.Path)).To(Equal(os.FileMode(0o600)))
	})

	It("has its directory made private too", func() {
		Expect(os.MkdirAll(dir, 0o700)).To(Succeed())
		Expect(os.Chmod(dir, 0o755)).To(Succeed())

		Expect(log.Append(entry("prod", "getFacility", 1))).To(Succeed())

		Expect(mode(dir)).To(Equal(os.FileMode(0o700)))
		Expect(mode(log.Path)).To(Equal(os.FileMode(0o600)))
	})
})
