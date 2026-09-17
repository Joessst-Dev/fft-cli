package main

import (
	"net/http"
	"path/filepath"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// awaitEnd reads r's events until run id has finished, and returns its last event.
func awaitEnd(r tui.Runner, id tui.RunID) tui.RunEvent {
	GinkgoHelper()
	for {
		var ev tui.RunEvent
		Eventually(r.Events()).WithTimeout(runTimeout).Should(Receive(&ev))
		if ev.ID == id && ev.State == tui.RunDone {
			return ev
		}
	}
}

var _ = Describe("templates in the TUI", func() {
	const body = `{"order":{"consumer":{"email":"old@example.de"},"id":9007199254740993}}`

	var (
		c       *cli
		dataDir string
	)

	BeforeEach(func() {
		c = newCLI()
		dataDir = GinkgoT().TempDir()
		c.setenv("XDG_DATA_HOME", dataDir)
		GinkgoT().Chdir(GinkgoT().TempDir())
	})

	userPath := func(name string) string {
		return filepath.Join(dataDir, "fft", "templates", name+".json")
	}

	// saveRush saves body as the template rush for addOrder, the way the Request
	// screen's t does, pinned to project.
	saveRush := func(r *cliRunner, project string) {
		GinkgoHelper()
		id := start(r, tui.Invocation{
			Args:    []string{"template", "save", "--operation", "addOrder", "--file", "-", "rush"},
			Stdin:   []byte(body),
			Project: project,
		})
		ev := awaitEnd(r, id)
		Expect(ev.Result.ExitCode).To(Equal(exitcode.OK), "stderr: %s", ev.Result.Stderr)
		Expect(ev.Invocation.Exclusive).To(BeTrue(), "a save must not run beside another")
	}

	// render renders rush with args, pinned to project.
	render := func(r *cliRunner, project string, args ...string) tui.Result {
		GinkgoHelper()
		id := start(r, tui.Invocation{
			Args:    append([]string{"template", "render", "rush"}, args...),
			Project: project,
		})
		res := awaitDone(r, id)[id]
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		return res
	}

	// sendWith sends stdin through the command the catalog names for addOrder.
	sendWith := func(r *cliRunner, project string, stdin []byte) tui.Result {
		GinkgoHelper()
		cmd := catalogOps(r.Catalog())["addOrder"].Command
		Expect(cmd.Body).To(BeTrue())
		id := start(r, tui.Invocation{
			Args:    append(slices.Clone(cmd.Path), "--file", "-"),
			Stdin:   stdin,
			Project: project,
		})
		return awaitDone(r, id)[id]
	}

	It("renders a saved template, and its output is what the operation's command sends", func() {
		t := c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
			answerJSON(w, `{"id":"order-1"}`)
		})
		r := c.newRunner()

		saveRush(r, "prod")
		Expect(userPath("rush")).To(BeAnExistingFile())

		res := render(r, "prod", "--set", "order.consumer.email=a@b.de")
		Expect(res.Stderr).To(BeEmpty(), "a template rendered for the project it was saved under warns about nothing")

		sent := sendWith(r, "prod", res.Stdout)
		Expect(sent.ExitCode).To(Equal(exitcode.OK), "stderr: %s", sent.Stderr)

		calls := t.recorded()
		Expect(calls).To(HaveLen(1))
		Expect(calls[0].Method).To(Equal(http.MethodPost))
		Expect(calls[0].Path).To(Equal("/prod/api/orders"))
	})

	It("carries the body to the tenant exactly, 64-bit ids and all", func() {
		t := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			answerJSON(w, `{"id":"order-1"}`)
		})
		r := c.newRunner()
		saveRush(r, "")

		res := render(r, "", "--set", "order.consumer.email=a@b.de")
		sent := sendWith(r, "", res.Stdout)
		Expect(sent.ExitCode).To(Equal(exitcode.OK), "stderr: %s", sent.Stderr)

		call := t.only()
		Expect(string(call.Body)).To(ContainSubstring("a@b.de"))
		Expect(string(call.Body)).To(ContainSubstring("9007199254740993"))
	})

	It("warns when the template is rendered for a project other than the one it was saved under", func() {
		t := c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
			answerJSON(w, `{}`)
		})
		r := c.newRunner()
		saveRush(r, "prod")

		res := render(r, "other")
		Expect(string(res.Stderr)).To(ContainSubstring(`rush was saved under project "prod", and the active project is "other"`))
		Expect(t.recorded()).To(BeEmpty(), "rendering reaches nothing")
	})

	It("refuses to send a rendered write to a read-only project, and sends nothing", func() {
		t := c.readOnlyProject(true)
		r := c.newRunner()
		r.SetProject("prod")
		saveRush(r, "prod")

		res := render(r, "prod")
		sent := sendWith(r, "prod", res.Stdout)

		Expect(sent.ExitCode).To(Equal(exitcode.ReadOnly))
		Expect(sent.Status).To(BeZero())
		Expect(string(sent.Stderr)).To(ContainSubstring(`project "prod" is read-only`))
		Expect(t.recorded()).To(BeEmpty(), "a read-only project was sent a request")
	})

	It("records the operation, refusing one this fft does not know", func() {
		r := c.newRunner()
		id := start(r, tui.Invocation{
			Args:  []string{"template", "save", "--operation", "noSuchOperation", "--file", "-", "rush"},
			Stdin: []byte(body),
		})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.Usage))
		Expect(string(res.Stderr)).To(ContainSubstring(`there is no operation "noSuchOperation"`))
		Expect(userPath("rush")).NotTo(BeAnExistingFile())
	})

	It("removes a template only once its question is answered, alone", func() {
		r := c.newRunner()
		saveRush(r, "")

		id := start(r, tui.Invocation{Args: []string{"template", "remove", "rush"}})
		q := awaitQuestion(r, id)
		Expect(q.Text).To(ContainSubstring("Remove the template rush"))
		Expect(q.Confirm).To(BeEmpty(), "a y answers it")
		Expect(userPath("rush")).To(BeAnExistingFile())

		r.Answer(id, q.ID, true)
		ev := awaitEnd(r, id)
		Expect(ev.Result.ExitCode).To(Equal(exitcode.OK), "stderr: %s", ev.Result.Stderr)
		Expect(ev.Invocation.Exclusive).To(BeTrue())
		Expect(userPath("rush")).NotTo(BeAnExistingFile())
	})

	It("keeps a template when its removal is declined", func() {
		r := c.newRunner()
		saveRush(r, "")

		id := start(r, tui.Invocation{Args: []string{"template", "remove", "rush"}})
		q := awaitQuestion(r, id)
		r.Answer(id, q.ID, false)

		Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.Usage))
		Expect(userPath("rush")).To(BeAnExistingFile())
	})
})
