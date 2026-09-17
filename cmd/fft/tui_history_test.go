package main

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/history"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

var _ = Describe("the TUI's request history", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
		c.withHistory()
	})

	// read reads the history the way the History screen does.
	read := func(r *cliRunner) ([]history.Entry, string) {
		GinkgoHelper()
		entries, off, err := r.History().Read()
		Expect(err).NotTo(HaveOccurred())
		return entries, off
	}

	When("a configured project is used", func() {
		BeforeEach(func() {
			c.historyTenant()
		})

		It("reads what the runs recorded, and says recording is on", func() {
			r := c.newRunner()
			entries, off := read(r)
			Expect(entries).To(BeEmpty())
			Expect(off).To(BeEmpty())

			id := start(r, tui.Invocation{Args: []string{"facility", "list", "--status", "ONLINE"}})
			Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))

			entries, _ = read(r)
			Expect(entries).To(ConsistOf(And(
				HaveField("Project", "prod"),
				HaveField("OperationID", "searchFacility"),
				HaveField("Command", "fft facility list"),
				HaveField("Args", []string{"--status=ONLINE"}),
				HaveField("Source", history.SourceTUI),
			)))
		})

		It("says why nothing is recorded when FFT_HISTORY is off, and still reads what was", func() {
			r := c.newRunner()
			id := start(r, tui.Invocation{Args: []string{"facility", "list"}})
			Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))

			c.setenv(config.EnvHistory, "off")
			entries, off := read(r)
			Expect(off).To(Equal("FFT_HISTORY is off"))
			Expect(entries).To(HaveLen(1))
		})

		It("says why nothing is recorded when the config file switches it off", func() {
			cfg, err := c.deps.Config.Load()
			Expect(err).NotTo(HaveOccurred())
			cfg.Settings.NoHistory = true
			Expect(c.deps.Config.Save(cfg)).To(Succeed())

			_, off := read(c.newRunner())
			Expect(off).To(ContainSubstring("settings.noHistory"))
		})

		It("reports a history file it cannot read", func() {
			Expect(os.MkdirAll(c.deps.HistoryPath, 0o700)).To(Succeed())

			_, _, err := c.newRunner().History().Read()
			Expect(err).To(MatchError(ContainSubstring(filepath.Base(c.deps.HistoryPath))))
		})
	})

	It("says recording is off by default in headless mode", func() {
		c.headless()
		_, off := read(c.newRunner())
		Expect(off).To(ContainSubstring("running from the environment"))
		Expect(off).To(ContainSubstring(config.EnvHistory + "=on"))
	})

	It("clears through the runner, asking first, and reads nothing after", func() {
		c.historyTenant()
		r := c.newRunner()
		id := start(r, tui.Invocation{Args: []string{"facility", "list"}})
		Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))

		clear := start(r, tui.Invocation{Args: []string{"history", "clear"}})
		var ev tui.RunEvent
		for ev.Question == nil {
			Eventually(r.Events()).WithTimeout(runTimeout).Should(Receive(&ev))
		}
		Expect(ev.Invocation.Exclusive).To(BeTrue())
		Expect(ev.Question.Text).To(ContainSubstring("Delete the whole request history?"))
		r.Answer(clear, ev.Question.ID, true)

		res := awaitDone(r, clear)[clear]
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(string(res.Stderr)).To(ContainSubstring("Cleared 1 entry"))

		entries, _ := read(r)
		Expect(entries).To(BeEmpty())
	})
})
