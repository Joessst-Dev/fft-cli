package tui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// sent sends op's form as it stands and returns the run it started, answering yes
// if the screen asks.
func (h *harness) sent(op Operation, fill ...string) RunID {
	GinkgoHelper()
	h.request(op)
	for i, v := range fill {
		h.fill(i, v)
	}
	h.press("s")
	if h.m.request.dialog != nil {
		h.press("y")
	}
	Expect(h.m.current).To(Equal(tabResponse))
	return RunID(len(h.r.started))
}

// wrote is the file at path, relative to the harness's temporary directory.
func (h *harness) wrote(name string) string {
	GinkgoHelper()
	raw, err := os.ReadFile(filepath.Join(h.tmp, name))
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}

// saveAs answers the save prompt with path.
func (h *harness) saveAs(path string) {
	GinkgoHelper()
	h.press("s")
	Expect(h.view()).To(ContainSubstring("Save the response body to:"))
	in := h.m.response.dialog.(*inputDialog)
	in.input.SetValue(path)
	h.press("enter")
}

var _ = Describe("the Response screen", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{})
		h.loaded(twoProjects, validToken)
	})

	It("says there is nothing to show before anything was sent", func() {
		h.press("4")
		Expect(h.view()).To(ContainSubstring("Nothing has been sent yet."))
	})

	Describe("while the request runs", func() {
		var id RunID

		BeforeEach(func() {
			id = h.sent(opGetPickJob, "pj-1")
			h.send(runEventMsg{ID: id, State: RunRunning, At: h.now})
			h.now = h.now.Add(1500 * time.Millisecond)
		})

		It("says it is running, and for how long", func() {
			Expect(h.view()).To(ContainSubstring("running 1.5s"))
			Expect(h.view()).To(ContainSubstring("$ fft picking get-pick-job --pick-job-id pj-1"))
			Expect(h.view()).To(ContainSubstring("c cancel"))
		})

		It("cancels it with c", func() {
			h.press("c")
			Expect(h.r.cancelled).To(Equal([]RunID{id}))
			Expect(h.view()).To(ContainSubstring("Cancelling"))
		})

		It("shows how it ended once it has", func() {
			h.finishID(id, Result{ExitCode: exitcode.Interrupted})
			Expect(h.view()).To(ContainSubstring("exit 130 (interrupted)"))
			Expect(h.view()).To(ContainSubstring("no HTTP response"))
		})

		It("says the cancel stopped it, once it has ended so", func() {
			h.press("c")
			h.finishID(id, Result{ExitCode: exitcode.Interrupted})

			Expect(h.view()).To(ContainSubstring("Cancelled. A write that had already reached the tenant may still have landed."))
			Expect(h.view()).NotTo(ContainSubstring("Cancelling"))
		})

		It("says when the run finished before the cancel reached it", func() {
			h.press("c")
			h.finishID(id, Result{ExitCode: exitcode.OK, Status: 200, Stdout: []byte(`{}`)})

			Expect(h.view()).To(ContainSubstring("exit 0 (success) · HTTP 200"))
			Expect(h.view()).To(ContainSubstring("It finished before the cancel reached it"))
		})
	})

	Describe("a finished response", func() {
		var id RunID

		BeforeEach(func() {
			id = h.sent(opListFacilities)
			h.finishID(id, Result{
				ExitCode: exitcode.OK,
				Status:   200,
				Duration: 132 * time.Millisecond,
				Stdout:   []byte(`[{"id":"f-1","name":"Berlin\u001b[2J"}]`),
				Stderr:   []byte("Total: 1\n\x1b]52;c;cGF5bG9hZA==\a"),
				Project:  "staging",
			})
		})

		It("heads it with the exit code and what it means, the HTTP status and the time", func() {
			Expect(h.view()).To(ContainSubstring("exit 0 (success) · HTTP 200 · 132ms · staging"))
		})

		It("shows the JSON indented", func() {
			view := h.view()
			Expect(view).To(ContainSubstring("[JSON]"))
			Expect(view).To(MatchRegexp(`\[\s*\n\s+\{\s*\n\s+"id": "f-1",`))
		})

		It("draws nothing the tenant sent as a terminal instruction", func() {
			content := h.m.View().Content
			Expect(content).NotTo(ContainSubstring("\x1b[2J"))

			h.press("right", "right")
			content = h.m.View().Content
			Expect(h.view()).To(ContainSubstring("[Stderr]"))
			Expect(h.view()).To(ContainSubstring("Total: 1"))
			Expect(content).NotTo(ContainSubstring("\x1b]52"))
			Expect(content).NotTo(ContainSubstring("\a"))
		})

		It("offers the table of a command that prints one", func() {
			h.press("right")
			Expect(h.view()).To(ContainSubstring("[Table]"))
			Expect(h.view()).To(ContainSubstring(`TABLE OF [{"id":"f-1"`))
		})

		It("offers no table for a command without one", func() {
			other := h.sent(opGetPickJob, "pj-1")
			h.finishID(other, ok(`{"id":"pj-1"}`))

			Expect(h.view()).NotTo(ContainSubstring("Table"))
			h.press("right")
			Expect(h.view()).To(ContainSubstring("[Stderr]"))
			Expect(h.view()).To(ContainSubstring("The command said nothing on stderr."))
		})

		It("shows the command it ran, to copy", func() {
			Expect(h.view()).To(ContainSubstring("$ fft facility list --project staging"))
			Expect(clipboard(h.press("y"))).To(Equal("fft facility list --project staging"))
		})

		It("goes back to the request with esc", func() {
			h.press("esc")
			Expect(h.m.current).To(Equal(tabRequest))
			Expect(h.view()).To(ContainSubstring("Search facilities"))
		})

		Describe("sending it again", func() {
			It("sends the same command line to the project it went to, and shows the new response", func() {
				h.press("r")

				again := h.last()
				Expect(again.Args).To(Equal([]string{"facility", "list"}))
				Expect(again.Project).To(Equal("staging"))
				Expect(h.m.response.id).To(Equal(RunID(len(h.r.started))))
				Expect(h.view()).To(ContainSubstring("$ fft facility list --project staging"))
			})

			It("still goes there after the UI has switched project", func() {
				h.m.projects.selectProject("prod")
				h.press("r")

				Expect(h.last().Project).To(Equal("staging"))
			})

			It("asks first when it is a write, and sends the same body", func() {
				h.request(opAddPickJob)
				h.m.request.body = []byte(`{"pickLineItems":[1]}`)
				h.press("s", "y")
				write := RunID(len(h.r.started))
				h.finishID(write, Result{ExitCode: exitcode.OK, Project: "staging"})

				n := len(h.r.started)
				h.press("r")
				Expect(h.r.started).To(HaveLen(n))
				Expect(h.view()).To(ContainSubstring("Send Create a pick job to staging again?"))

				h.press("y")
				Expect(h.r.started).To(HaveLen(n + 1))
				Expect(string(h.last().Stdin)).To(Equal(`{"pickLineItems":[1]}`))
				Expect(h.last().Args).To(Equal([]string{"picking", "add-pick-job", "--file", "-"}))
			})

			It("is not offered for a run the Request screen did not send", func() {
				// Newest first: the one below the request is the credential check.
				h.press("1", "i", "down", "enter")
				Expect(h.m.current).To(Equal(tabResponse))
				Expect(h.view()).NotTo(ContainSubstring("r send again"))

				n := len(h.r.started)
				h.press("r")
				Expect(h.r.started).To(HaveLen(n))
				Expect(h.view()).To(ContainSubstring("Only a request sent from the Request screen can be sent again"))
			})
		})

		Describe("saving the body", func() {
			BeforeEach(func() {
				// A relative name is relative to where fft was started.
				GinkgoT().Chdir(h.tmp)
			})

			It("proposes a name, and writes the body privately", func() {
				h.press("s")
				Expect(h.view()).To(ContainSubstring("> response-3.json"))
				h.press("enter")

				Expect(h.wrote("response-3.json")).To(Equal(`[{"id":"f-1","name":"Berlin\u001b[2J"}]`))
				info, err := os.Stat(filepath.Join(h.tmp, "response-3.json"))
				Expect(err).NotTo(HaveOccurred())
				Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
				Expect(h.view()).To(ContainSubstring("Saved 39 bytes to "))
			})

			It("asks before it replaces a file, and leaves it alone on no", func() {
				Expect(os.WriteFile(filepath.Join(h.tmp, "out.json"), []byte("keep"), 0o600)).To(Succeed())

				h.saveAs("out.json")
				Expect(h.view()).To(ContainSubstring("out.json?"))
				h.press("n")
				Expect(h.wrote("out.json")).To(Equal("keep"))

				h.saveAs("out.json")
				h.press("y")
				Expect(h.wrote("out.json")).To(HavePrefix(`[{"id":"f-1"`))
			})

			It("replaces a link rather than writing through it", func() {
				target := filepath.Join(h.tmp, "elsewhere.txt")
				Expect(os.WriteFile(target, []byte("untouched"), 0o600)).To(Succeed())
				Expect(os.Symlink(target, filepath.Join(h.tmp, "link.json"))).To(Succeed())

				h.saveAs("link.json")

				Expect(h.view()).To(ContainSubstring("is not a plain file"))
				Expect(h.wrote("elsewhere.txt")).To(Equal("untouched"))
			})

			It("refuses a directory it would have to create", func() {
				h.saveAs("missing/out.json")

				Expect(h.view()).To(ContainSubstring("the directory must already exist"))
				Expect(filepath.Join(h.tmp, "missing")).NotTo(BeADirectory())
			})

			It("refuses a directory in place of the file", func() {
				Expect(os.Mkdir(filepath.Join(h.tmp, "dir.json"), 0o700)).To(Succeed())
				h.saveAs("dir.json")

				Expect(h.view()).To(ContainSubstring("is not a plain file"))
			})

			It("does nothing when the prompt is cancelled", func() {
				h.press("s", "esc")
				Expect(h.files()).To(BeEmpty())
			})
		})
	})

	Describe("a response it cannot show whole", func() {
		It("says the response was cut short, and will not save it", func() {
			id := h.sent(opGetPickJob, "pj-1")
			// Cut off, it is no longer JSON, and a raw escape in it is not escaped.
			h.finishID(id, Result{ExitCode: exitcode.OK, Stdout: []byte("{\"id\":\"pj-1\",\"note\":\"\x1b[2Jxx"), StdoutTruncated: true})

			Expect(h.view()).To(ContainSubstring("Only the first 27 bytes of the response is shown"))
			Expect(h.view()).To(ContainSubstring(`{"id":"pj-1","note":"[2Jxx`))
			Expect(h.m.View().Content).NotTo(ContainSubstring("\x1b[2J"))
			h.press("s")
			Expect(h.m.response.dialog).To(BeNil())
			Expect(h.view()).To(ContainSubstring("too large to keep whole"))
		})

		It("says a long stderr was cut short", func() {
			id := h.sent(opGetPickJob, "pj-1")
			h.finishID(id, Result{ExitCode: exitcode.General, Stderr: []byte("Error: …"), StderrTruncated: true})

			Expect(h.view()).To(ContainSubstring("Stderr was too long to keep whole"))
		})

		It("describes a binary body rather than drawing it", func() {
			id := h.sent(opGetPickJob, "pj-1")
			h.finishID(id, ok("%PDF-1.7\x00\x01\x02"))

			Expect(h.view()).To(ContainSubstring("The response is 11 bytes of binary data"))
			Expect(h.m.View().Content).NotTo(ContainSubstring("\x00"))
		})

		It("says an older response is no longer held, once the session's budget is spent", func() {
			first := h.sent(opGetPickJob, "pj-1")
			h.finishID(first, ok(`{"id":"pj-1"}`))
			second := h.sent(opGetPickJob, "pj-2")
			h.finishID(second, ok(strings.Repeat(" ", keptOutputBytes)))

			h.m.openResponse(first)
			Expect(h.view()).To(ContainSubstring("This response is no longer held in memory"))
			h.press("s")
			Expect(h.m.response.dialog).To(BeNil())
		})
	})

	It("falls back to the JSON when the request behind the table is no longer kept", func() {
		id := h.sent(opListFacilities)
		h.finishID(id, ok(`[]`))
		h.press("right")
		Expect(h.view()).To(ContainSubstring("[Table]"))

		delete(h.m.s.requests, id)
		Expect(h.view()).To(ContainSubstring("[JSON]"))
	})

	It("says so when the run on display is no longer kept at all", func() {
		id := h.sent(opListFacilities)
		h.finishID(id, ok(`[]`))

		delete(h.m.s.runs.byID, id)
		Expect(h.view()).To(ContainSubstring("That run is no longer kept"))
	})

	Describe("from the command panel", func() {
		It("opens the response of the selected run, and closes the panel", func() {
			h.press("i")
			h.press("down", "enter")

			Expect(h.m.showPanel).To(BeFalse())
			Expect(h.m.current).To(Equal(tabResponse))
			Expect(h.view()).To(ContainSubstring("$ fft project list"))
			Expect(h.view()).To(ContainSubstring("exit 0 (success)"))
		})
	})
})

var _ = Describe("the session's hold on output", func() {
	It("lets the oldest outputs go first, and keeps the newest within the budget", func() {
		l := newRunList()
		for id := RunID(1); id <= 3; id++ {
			l.add(id, shellCommand{}, time.Time{})
			l.update(RunEvent{ID: id, State: RunDone, Result: Result{Stdout: make([]byte, 40)}})
		}
		l.shed(100)

		Expect(l.byID[1].dropped).To(BeTrue())
		Expect(l.byID[1].result.Stdout).To(BeNil())
		Expect(l.byID[2].dropped).To(BeFalse())
		Expect(l.byID[3].result.Stdout).To(HaveLen(40))
	})

	It("keeps a run's exit code when it lets its output go", func() {
		l := newRunList()
		l.add(1, shellCommand{}, time.Time{})
		l.update(RunEvent{ID: 1, State: RunDone, Result: Result{ExitCode: exitcode.Conflict, Stderr: []byte("409")}})
		l.shed(1)

		Expect(l.byID[1].result.ExitCode).To(Equal(exitcode.Conflict))
	})
})
