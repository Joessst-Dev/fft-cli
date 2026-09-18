//go:build unix

package main

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("a history path that holds a FIFO", func() {
	// fifoCLI is a configured CLI whose history path is a FIFO nothing reads. A run
	// that opened it blocking would wait there for good; the cleanup opens both ends
	// without blocking, which releases it, so that a failing spec ends.
	fifoCLI := func() *cli {
		c := newCLI()
		log := c.withHistory()
		c.historyTenant()
		Expect(os.MkdirAll(filepath.Dir(log.Path), 0o700)).To(Succeed())
		Expect(unix.Mkfifo(log.Path, 0o600)).To(Succeed())
		DeferCleanup(func() {
			for _, flag := range []int{os.O_RDONLY, os.O_WRONLY} {
				if f, err := os.OpenFile(log.Path, flag|unix.O_NONBLOCK, 0); err == nil {
					_ = f.Close()
				}
			}
		})
		return c
	}

	// promptly runs args, and fails the spec if the run has not ended within a few
	// seconds.
	promptly := func(c *cli, args ...string) int {
		GinkgoHelper()
		done := make(chan int, 1)
		go func() { done <- c.run(args...) }()
		var code int
		Eventually(done).WithTimeout(5*time.Second).Should(Receive(&code), "the run is still waiting on the FIFO")
		return code
	}

	It("does not hold up a command, or change what it prints and how it exits", func() {
		plain := newCLI()
		plain.withHistory()
		plain.historyTenant()
		plain.setenv(config.EnvHistory, "off")
		want := plain.run("facility", "list", "-o", "json")

		c := fifoCLI()
		Expect(promptly(c, "facility", "list", "-o", "json")).To(Equal(want))
		Expect(c.out()).To(Equal(plain.out()))
		Expect(c.errOut()).To(Equal(plain.errOut()))
	})

	It("is reported by fft history list, which does not wait on it", func() {
		c := fifoCLI()
		Expect(promptly(c, "history", "list")).To(Equal(exitcode.General))
		Expect(c.out()).To(BeEmpty())
		Expect(c.errOut()).To(ContainSubstring("not a regular file"))
	})

	It("is reported by the TUI's history read, which does not wait on it", func() {
		c := fifoCLI()
		done := make(chan error, 1)
		go func() {
			_, _, err := c.newRunner().History().Read()
			done <- err
		}()
		var err error
		Eventually(done).WithTimeout(5 * time.Second).Should(Receive(&err))
		Expect(err).To(MatchError(ContainSubstring("not a regular file")))
	})
})
