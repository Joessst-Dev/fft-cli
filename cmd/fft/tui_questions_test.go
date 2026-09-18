package main

import (
	"context"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// awaitQuestion reads r's events until run id asks a question, and returns it.
func awaitQuestion(r tui.Runner, id tui.RunID) tui.Question {
	GinkgoHelper()
	for {
		var ev tui.RunEvent
		Eventually(r.Events()).WithTimeout(runTimeout).Should(Receive(&ev))
		Expect(ev.ID != id || ev.State != tui.RunDone).To(BeTrue(),
			"run %d ended without asking: exit %d, %s", id, ev.Result.ExitCode, ev.Result.Stderr)
		if ev.ID == id && ev.Question != nil {
			Expect(ev.State).To(Equal(tui.RunRunning))
			return *ev.Question
		}
	}
}

var _ = Describe("a question a command asks in the TUI", func() {
	var (
		c *cli
		t *tenant
		r *cliRunner
	)

	deleteFacility := tui.Invocation{Args: []string{"facility", "delete", "BER-01"}}

	BeforeEach(func() {
		c = newCLI()
		t = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.WriteHeader(http.StatusNoContent)
		})
		r = c.newRunner()
	})

	deletes := func() []call {
		var out []call
		for _, sent := range t.recorded() {
			if sent.Method == http.MethodDelete {
				out = append(out, sent)
			}
		}
		return out
	}

	It("is the command's own question, asked in the UI, with the word it must be answered with", func() {
		id := start(r, deleteFacility)
		q := awaitQuestion(r, id)

		Expect(q.Text).To(Equal("Delete facility urn:fft:facility:tenantFacilityId:BER-01? This cannot be undone."))
		Expect(q.Confirm).To(Equal("delete"))
		Expect(t.recorded()).To(BeEmpty(), "the facility was deleted before anyone answered")

		r.Answer(id, q.ID, true)
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(deletes()).To(HaveLen(1))
		Expect(string(res.Stderr)).To(ContainSubstring("This cannot be undone. [y/N]: y"))
	})

	It("is asked even when FFT_YES answers every question in the shell", func() {
		c.setenv("FFT_YES", "true")

		id := start(r, deleteFacility)
		q := awaitQuestion(r, id)
		Expect(t.recorded()).To(BeEmpty(), "the facility was deleted before anyone answered")

		r.Answer(id, q.ID, false)
		Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))
		Expect(deletes()).To(BeEmpty())
	})

	It("is not asked when the UI passed --yes after asking itself", func() {
		id := start(r, tui.Invocation{Args: []string{"facility", "delete", "BER-01", "--yes"}})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(deletes()).To(HaveLen(1))
	})

	It("sends nothing on no", func() {
		id := start(r, deleteFacility)
		q := awaitQuestion(r, id)

		r.Answer(id, q.ID, false)
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(string(res.Stderr)).To(ContainSubstring("Aborted"))
		Expect(t.recorded()).To(BeEmpty())
	})

	It("takes no answer meant for another question, or for another run", func() {
		id := start(r, deleteFacility)
		q := awaitQuestion(r, id)

		r.Answer(id, q.ID+1, true)
		r.Answer(id+1, q.ID, true)
		Consistently(t.recorded).WithTimeout(100 * time.Millisecond).Should(BeEmpty())

		r.Answer(id, q.ID, false)
		Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))
		Expect(t.recorded()).To(BeEmpty())
	})

	It("is answered no when the run is cancelled, whatever is answered after", func() {
		id := start(r, deleteFacility)
		q := awaitQuestion(r, id)

		r.Cancel(id)
		r.Answer(id, q.ID, true)
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.Interrupted), "stderr: %s", res.Stderr)
		Expect(t.recorded()).To(BeEmpty())
	})

	It("does not keep the runner from shutting down", func() {
		runner := newCLIRunner(context.Background(), c.deps, uiRun{})
		id := start(runner, deleteFacility)
		awaitQuestion(runner, id)

		closed := make(chan struct{})
		go func() {
			defer close(closed)
			runner.Close()
		}()
		Eventually(closed).WithTimeout(runTimeout).Should(BeClosed())
		Expect(t.recorded()).To(BeEmpty())
	})

	It("is answered with a y when what it asks about can be undone", func() {
		c = newCLI()
		c.configuredTenant(func(http.ResponseWriter, *http.Request) {})
		r = c.newRunner()

		id := start(r, tui.Invocation{Args: []string{"project", "remove", "other"}})
		q := awaitQuestion(r, id)
		Expect(q.Text).To(ContainSubstring(`Remove project "other"`))
		Expect(q.Confirm).To(BeEmpty())

		r.Answer(id, q.ID, false)
		Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))
		cfg, err := c.deps.Config.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Resolve("other")).Error().NotTo(HaveOccurred(), "the project was removed on no")
	})

	It("is not asked of a command given --yes on its own command line", func() {
		id := start(r, tui.Invocation{Args: []string{"facility", "delete", "BER-01", "--yes"}})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(deletes()).To(HaveLen(1))
	})

	It("still leaves every other prompt without a terminal to ask on", func() {
		id := start(r, tui.Invocation{Args: []string{"template", "render"}})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.Usage), "stderr: %s", res.Stderr)
	})
})
