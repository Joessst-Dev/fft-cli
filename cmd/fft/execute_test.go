package main

import (
	"context"
	"io"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// captureProcessStreams points the process's stdout and stderr at pipes for the
// rest of the spec, and returns a function that restores them and reports what was
// written to each.
func captureProcessStreams() func() (stdout, stderr string) {
	GinkgoHelper()

	drain := func(target **os.File) func() string {
		r, w, err := os.Pipe()
		Expect(err).NotTo(HaveOccurred())
		original := *target
		*target = w

		got := make(chan string, 1)
		go func() {
			defer GinkgoRecover()
			data, err := io.ReadAll(r)
			Expect(err).NotTo(HaveOccurred())
			got <- string(data)
		}()

		restore := func() string {
			*target = original
			Expect(w.Close()).To(Succeed())
			text := <-got
			Expect(r.Close()).To(Succeed())
			return text
		}
		// Restored even when the spec fails before it asks, so that no later spec
		// writes into a pipe nobody reads.
		DeferCleanup(func() {
			if *target == w {
				restore()
			}
		})
		return restore
	}

	outDone := drain(&os.Stdout)
	errDone := drain(&os.Stderr)
	return func() (string, string) { return outDone(), errDone() }
}

// The process's own streams are cobra's defaults, and cobra's defaults are not
// "stdout for output": its Print family — an unknown help topic, among others —
// writes to stderr unless an output stream was set explicitly. So a funnel that
// sets the process's stdout explicitly moves those messages onto stdout, and into
// whatever a script is piping it to.
var _ = Describe("a command line run on the process's own streams", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("reports an unknown help topic on stderr, leaving stdout empty", func() {
		finish := captureProcessStreams()
		code := runProcess(context.Background(), c.deps, []string{"help", "nonsense"})
		stdout, stderr := finish()

		Expect(code).To(Equal(exitcode.OK))
		Expect(stdout).To(BeEmpty())
		Expect(stderr).To(ContainSubstring("Unknown help topic"))
	})

	It("still prints help that was asked for on stdout", func() {
		finish := captureProcessStreams()
		code := runProcess(context.Background(), c.deps, []string{"--help"})
		stdout, stderr := finish()

		Expect(code).To(Equal(exitcode.OK))
		Expect(stdout).To(ContainSubstring("Usage:"))
		Expect(stderr).To(BeEmpty())
	})

	It("reports a failure on stderr", func() {
		finish := captureProcessStreams()
		code := runProcess(context.Background(), c.deps, []string{"facility", "--bogus"})
		stdout, stderr := finish()

		Expect(code).To(Equal(exitcode.Usage))
		Expect(stdout).To(BeEmpty())
		Expect(stderr).To(ContainSubstring("unknown flag: --bogus"))
	})
})
