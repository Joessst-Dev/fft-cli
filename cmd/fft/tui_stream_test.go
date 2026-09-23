package main

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// awaitStreamed reads r's events until run id is done, and returns what arrived on
// each stream as chunks, with the result.
func awaitStreamed(r tui.Runner, id tui.RunID) (stdout, stderr string, res tui.Result) {
	GinkgoHelper()
	var out, errOut strings.Builder
	for {
		var ev tui.RunEvent
		Eventually(r.Events()).WithTimeout(runTimeout).Should(Receive(&ev))
		if ev.ID != id {
			continue
		}
		if ev.Chunk != nil {
			Expect(ev.State).To(Equal(tui.RunRunning), "output is not a change of state")
			Expect(ev.Question).To(BeNil())
			if ev.Chunk.Stderr {
				errOut.Write(ev.Chunk.Bytes)
			} else {
				out.Write(ev.Chunk.Bytes)
			}
			continue
		}
		if ev.State == tui.RunDone {
			return out.String(), errOut.String(), ev.Result
		}
	}
}

var _ = Describe("a streaming run in the TUI", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
		c.installFake(fakeManifest("weather"))
	})

	It("reports the output as it is produced, and the same bytes end up in the result", func() {
		r := c.newRunner()
		id := start(r, tui.Invocation{Args: []string{"weather"}, Stream: true})

		stdout, stderr, res := awaitStreamed(r, id)
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)

		Expect(stdout).NotTo(BeEmpty())
		Expect(stdout).To(Equal(string(res.Stdout)))
		Expect(stderr).To(Equal(string(res.Stderr)))
		Expect(stderr).To(ContainSubstring("fake component weather speaking"))
	})

	It("says nothing as it goes unless it was asked to", func() {
		r := c.newRunner()
		id := start(r, tui.Invocation{Args: []string{"version"}})

		stdout, stderr, res := awaitStreamed(r, id)
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(stdout).To(BeEmpty())
		Expect(stderr).To(BeEmpty())
		Expect(res.Stdout).NotTo(BeEmpty(), "the result still has all of it")
	})

	It("refuses a component it would not be showing", func() {
		r := c.newRunner()
		id := start(r, tui.Invocation{Args: []string{"weather"}})

		res := awaitDone(r, id)[id]
		Expect(res.ExitCode).To(Equal(exitcode.Usage))
		Expect(string(res.Stderr)).To(ContainSubstring("only shows for a run it is watching"))
	})
})

var _ = Describe("the streaming writer", func() {
	It("passes everything through to the run's own buffer", func() {
		buf := &cappedBuffer{limit: 64}
		w := &streamWriter{to: buf, limit: 64}

		n, err := w.Write([]byte("hello"))
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(5))
		Expect(string(buf.buf)).To(Equal("hello"))
	})

	It("hands over what was written, and starts again from empty", func() {
		w := &streamWriter{to: &cappedBuffer{limit: 64}, limit: 64, stderr: true}
		_, _ = w.Write([]byte("one"))
		_, _ = w.Write([]byte("two"))

		c := w.take()
		Expect(c).NotTo(BeNil())
		Expect(c.Stderr).To(BeTrue())
		Expect(string(c.Bytes)).To(Equal("onetwo"))
		Expect(c.Dropped).To(BeFalse())

		Expect(w.take()).To(BeNil(), "nothing has been written since")
	})

	It("keeps the newest bytes past its limit, and says it dropped the rest", func() {
		w := &streamWriter{to: &cappedBuffer{limit: 1 << 20}, limit: 8}
		_, _ = w.Write([]byte("0123456789abc"))

		c := w.take()
		Expect(string(c.Bytes)).To(Equal("56789abc"), "the tail is what a live log shows")
		Expect(c.Dropped).To(BeTrue(), "a log tail that quietly loses lines is worse than one that says so")
	})

	It("remembers a chunk the UI never took", func() {
		w := &streamWriter{to: &cappedBuffer{limit: 64}, limit: 64}
		w.drop()
		_, _ = w.Write([]byte("after"))

		c := w.take()
		Expect(string(c.Bytes)).To(Equal("after"))
		Expect(c.Dropped).To(BeTrue())
	})
})

var _ = Describe("the component registry inside the TUI", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	// localComponent is a directory `component install --path` will accept.
	localComponent := func(name string) string {
		GinkgoHelper()
		src := GinkgoT().TempDir()
		m := fakeManifest(name)
		writeManifestFile(src, m)
		Expect(os.MkdirAll(filepath.Join(src, "bin"), 0o755)).To(Succeed())
		Expect(copyExecutable(fakeComponentBinary(), componentExecPath(src, m.Exec))).To(Succeed())
		c.setenv(envFakeComponent, "1")
		return src
	}

	It("is read again after an install, so the new component is listed and can be run", func() {
		src := localComponent("weather")
		// The root has to exist before the runner takes its first scan, or the
		// registry it starts with is one of a directory that was never there.
		c.componentRoot()
		r := c.newRunner()

		install := start(r, tui.Invocation{Args: []string{"component", "install", "--path", src}})
		q := awaitQuestion(r, install)
		Expect(q.Text).To(ContainSubstring("weather"))
		r.Answer(install, q.ID, true)
		Expect(awaitDone(r, install)[install].ExitCode).To(Equal(exitcode.OK))

		list := start(r, tui.Invocation{Args: []string{"component", "list"}})
		res := awaitDone(r, list)[list]
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(string(res.Stdout)).To(ContainSubstring("weather"),
			"a component installed in the UI must not need a restart to be seen")

		// And its command is in the tree the next run builds.
		run := start(r, tui.Invocation{Args: []string{"weather"}, Stream: true})
		Expect(awaitDone(r, run)[run].ExitCode).To(Equal(exitcode.OK))
	})

	It("keeps the registry it has when a rescan would find nothing", func() {
		c.installFake(fakeManifest("weather"))
		r := c.newRunner()

		before := r.components.Load()
		Expect(before).NotTo(BeNil())
		_, found := before.Lookup("weather")
		Expect(found).To(BeTrue())

		// A root that cannot be read must not replace a good scan with an empty one.
		Expect(os.RemoveAll(before.Root())).To(Succeed())
		r.rescanComponents()
		Expect(r.components.Load()).NotTo(BeNil())
	})

	It("does nothing when components are disabled", func() {
		c.setenv(component.EnvRoot, "")
		r := c.newRunner()

		was := r.components.Load()
		r.rescanComponents()
		Expect(r.components.Load()).To(BeIdenticalTo(was), "there is no root to read")
	})
})
