package tui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// request opens the Request screen on op, as enter on the Operations screen does.
func (h *harness) request(op Operation) {
	GinkgoHelper()
	h.send(h.m.openRequest(op))
	Expect(h.m.current).To(Equal(tabRequest))
}

// fill types value into the form's field at row, the way a user does.
func (h *harness) fill(row int, value string) {
	GinkgoHelper()
	h.m.request.cursor = row
	h.press("enter")
	Expect(h.m.request.editing).To(BeTrue())
	h.typeText(value)
	h.press("enter")
}

// last is the invocation started last.
func (h *harness) last() Invocation {
	GinkgoHelper()
	Expect(h.r.started).NotTo(BeEmpty())
	return h.r.started[len(h.r.started)-1]
}

var _ = Describe("the Request screen", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{})
		h.loaded(twoProjects, validToken)
	})

	It("asks for an operation before there is one", func() {
		h.press("3")
		Expect(h.view()).To(ContainSubstring("Choose an operation on the Operations screen"))
	})

	Describe("the form", func() {
		BeforeEach(func() {
			h.request(opListFacilities)
		})

		It("has a row per flag, with what each takes", func() {
			view := h.view()
			Expect(view).To(ContainSubstring("Search facilities"))
			Expect(view).To(ContainSubstring("POST /api/facilities/search · searchFacility"))
			Expect(view).To(MatchRegexp(`> --status\s+ONLINE \| OFFLINE`))
			Expect(view).To(MatchRegexp(`--size\s+integer, default 20`))
			Expect(view).To(MatchRegexp(`--all\s+\[ \]`))
			Expect(view).To(ContainSubstring("Only facilities in this status"))
			Expect(view).To(ContainSubstring("Body: this command takes none."))
		})

		It("marks what is required, arguments first", func() {
			h.request(opDeleteFacility)
			Expect(h.view()).To(MatchRegexp(`> <id> \(required\)`))

			h.request(opGetPickJob)
			Expect(h.view()).To(ContainSubstring("--pick-job-id (required)"))
		})

		It("shows the command it stands for as it is filled in", func() {
			Expect(h.view()).To(ContainSubstring("$ fft facility list"))

			h.fill(0, "ONLINE, OFFLINE")
			h.fill(1, "50")
			h.m.request.cursor = 2
			h.press("space")

			Expect(h.view()).To(ContainSubstring("$ fft facility list --status ONLINE --status OFFLINE --size 50 --all"))
		})

		It("cycles an on/off field through on, an explicit off, and unset", func() {
			h.m.request.cursor = 2
			Expect(h.view()).To(MatchRegexp(`--all\s+\[ \] unset`))

			h.press("space")
			Expect(h.view()).To(MatchRegexp(`--all\s+\[x\] on`))
			Expect(h.view()).To(ContainSubstring("$ fft facility list --all --project staging"))

			h.press("space")
			Expect(h.view()).To(MatchRegexp(`--all\s+\[-\] off`))
			Expect(h.view()).To(ContainSubstring("$ fft facility list --all=false --project staging"))
			h.press("s")
			Expect(h.last().Args).To(Equal([]string{"facility", "list", "--all=false"}))

			h.press("3")
			h.press("space")
			Expect(h.view()).To(ContainSubstring("$ fft facility list --project staging"))
		})

		It("leaves a default on to the command until the field is set", func() {
			op := opListFacilities
			op.Command.Flags = []Flag{{Name: "wait", Kind: FlagBool, Default: "true"}}
			h.request(op)
			Expect(h.view()).To(MatchRegexp(`--wait\s+\[ \] unset, default true`))
			Expect(h.view()).To(ContainSubstring("$ fft facility list --project staging"))

			h.press("space", "space")
			Expect(h.view()).To(ContainSubstring("$ fft facility list --wait=false --project staging"))
			h.press("x")
			Expect(h.view()).To(ContainSubstring("$ fft facility list --project staging"))
		})

		It("takes every key as text while a field is being edited", func() {
			h.m.request.cursor = 1
			h.press("enter")
			h.typeText("q2iy")

			Expect(h.m.owner()).To(Equal(ownerFocused))
			Expect(h.view()).To(ContainSubstring("[3 Request]"))
			Expect(h.m.confirmQuit).To(BeFalse())
			Expect(h.view()).To(ContainSubstring("enter done"))
			Expect(h.view()).NotTo(ContainSubstring("$ fft"), "a field being edited offers no command to copy")
		})

		It("puts back what the field held when esc leaves it", func() {
			h.fill(1, "50")
			h.press("enter")
			h.typeText("0")
			h.press("esc")

			Expect(h.m.request.fields[1].value()).To(Equal("50"))
		})

		It("moves between fields with tab while editing, and stops at an on/off row", func() {
			h.press("enter")
			h.typeText("ONLINE")
			h.press("tab")
			Expect(h.m.request.editing).To(BeTrue())
			h.typeText("5")
			h.press("tab")

			Expect(h.m.request.editing).To(BeFalse())
			Expect(h.m.request.cursor).To(Equal(2))
			Expect(h.view()).To(ContainSubstring("--status ONLINE --size 5"))
		})

		It("drops the line break a pasted value brings along", func() {
			h.press("enter")
			h.send(tea.PasteMsg{Content: "ONLINE\n"})
			h.press("enter")

			Expect(h.m.request.fields[0].value()).To(Equal("ONLINE"))
		})

		It("clears a field with x", func() {
			h.fill(1, "50")
			h.press("x")
			Expect(h.m.request.fields[1].value()).To(BeEmpty())
			Expect(h.view()).NotTo(ContainSubstring("--size 50"))
		})

		It("goes back to the operations with esc", func() {
			h.press("esc")
			Expect(h.m.current).To(Equal(tabOperations))
		})
	})

	Describe("sending a read", func() {
		BeforeEach(func() {
			h.request(opListFacilities)
		})

		It("runs the command line the form describes, without asking, and shows its response", func() {
			h.fill(1, "50")
			h.press("s")

			inv := h.last()
			Expect(inv.Args).To(Equal([]string{"facility", "list", "--size", "50"}))
			Expect(inv.Stdin).To(BeNil())
			Expect(inv.Exclusive).To(BeFalse())

			Expect(h.m.current).To(Equal(tabResponse))
			Expect(h.view()).To(ContainSubstring("Press c to cancel it."))
		})

		It("refuses what the command would refuse, and sends nothing", func() {
			h.fill(1, "fifty")
			n := len(h.r.started)
			h.press("s")

			Expect(h.r.started).To(HaveLen(n))
			Expect(h.view()).To(ContainSubstring(`--size wants integer, not "fifty"`))
		})

		It("refuses a request without a required value", func() {
			h.request(opGetPickJob)
			n := len(h.r.started)
			h.press("s")

			Expect(h.r.started).To(HaveLen(n))
			Expect(h.view()).To(ContainSubstring("--pick-job-id is required"))
		})

		It("sends from a field being edited with ctrl+s", func() {
			h.request(opGetPickJob)
			h.press("enter")
			h.typeText("pj-1")
			h.press("ctrl+s")

			Expect(h.last().Args).To(Equal([]string{"picking", "get-pick-job", "--pick-job-id", "pj-1"}))
		})

		It("keeps a name=value pair whole, commas and all, and starts a new one at the next name=", func() {
			h.request(opAPIGetPickJobs)
			Expect(h.view()).To(MatchRegexp(`--query\s+name=value, comma-separated`))
			h.fill(2, "status=OPEN,CLOSED, orderRef=a,b=")

			h.press("s")
			Expect(h.last().Args).To(Equal([]string{
				"api", "getPickJobs", "--query", "status=OPEN,CLOSED", "--query", "orderRef=a", "--query", "b=",
			}))
		})

		It("joins a value that starts with a dash to its flag", func() {
			h.request(opGetPickJob)
			h.fill(0, "-1")
			h.press("s")

			Expect(h.last().Args).To(Equal([]string{"picking", "get-pick-job", "--pick-job-id=-1"}))
		})

		It("pins it to the project the UI selected, and shows that project", func() {
			h = newHarness(Options{Project: "staging"})
			h.loaded(twoProjects, validToken)
			h.request(opGetPickJob)
			h.fill(0, "pj-1")
			h.press("s")

			Expect(h.last().Project).To(Equal("staging"))
			Expect(h.last().Args).NotTo(ContainElement("--project"), "the project travels beside the command line")
			Expect(h.view()).To(ContainSubstring("fft picking get-pick-job --pick-job-id pj-1 --project staging"))
		})

		It("pins it to the active project when the UI has selected none", func() {
			h.request(opGetPickJob)
			h.fill(0, "pj-1")
			Expect(h.view()).To(ContainSubstring("$ fft picking get-pick-job --pick-job-id pj-1 --project staging"))
			h.press("s")

			Expect(h.last().Project).To(Equal("staging"))
		})

		It("leaves the project to the environment when fft runs headless", func() {
			h = newHarness(Options{Headless: true})
			h.loaded(`[{"name":"env","active":true,"baseUrl":"https://env.example.com","credential":"env"}]`, validToken)
			h.request(opGetPickJob)
			h.fill(0, "pj-1")
			h.press("s")

			Expect(h.last().Project).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("$ fft picking get-pick-job --pick-job-id pj-1"))
			Expect(h.view()).NotTo(ContainSubstring("--project"))
		})
	})

	Describe("sending a write", func() {
		BeforeEach(func() {
			h.request(opUnlockOrder)
			h.fill(0, "ORD-1")
		})

		It("asks first, showing the command a yes runs, and sends nothing yet", func() {
			n := len(h.r.started)
			h.press("s")

			Expect(h.r.started).To(HaveLen(n))
			view := h.view()
			Expect(view).To(ContainSubstring("Send Unlock an order to staging?"))
			Expect(view).To(ContainSubstring("POST /api/orders/{orderId}/actions changes data on the tenant."))
			Expect(view).To(ContainSubstring("runs: fft order unlock ORD-1 --project staging"))
			Expect(h.m.owner()).To(Equal(ownerFocused))
		})

		It("sends it to the project the question named, whatever is selected by the time it goes", func() {
			h.press("s")
			Expect(h.view()).To(ContainSubstring("Send Unlock an order to staging?"))
			// A switch the user asked for before, finishing while the question is open.
			h.m.s.selectProject("prod")
			h.wait()
			h.press("y")

			Expect(h.last().Project).To(Equal("staging"))
			Expect(h.m.response.entry().display.String()).To(ContainSubstring("--project staging"))
		})

		It("sends nothing on no", func() {
			n := len(h.r.started)
			h.press("s")
			h.wait()
			h.press("n")

			Expect(h.r.started).To(HaveLen(n))
			Expect(h.m.current).To(Equal(tabRequest))
		})

		It("sends nothing on a pasted yes", func() {
			n := len(h.r.started)
			h.press("s")
			h.wait()
			h.send(tea.PasteMsg{Content: "y"})

			Expect(h.r.started).To(HaveLen(n))
		})

		It("sends it once confirmed, with no --yes", func() {
			h.press("s")
			h.wait()
			h.press("y")

			Expect(h.last().Args).To(Equal([]string{"order", "unlock", "ORD-1"}))
			Expect(h.m.current).To(Equal(tabResponse))
		})

		It("takes no key until the question has been in front of the user for a moment", func() {
			n := len(h.r.started)
			h.press("s", "y")
			Expect(h.r.started).To(HaveLen(n), "a y pressed straight after s answered a question nobody had read")
			Expect(h.m.owner()).To(Equal(ownerFocused))

			h.wait()
			h.press("y")
			Expect(h.r.started).To(HaveLen(n + 1))
		})

		It("does not take a y typed straight after ctrl+s sent the field being edited", func() {
			n := len(h.r.started)
			h.press("x", "enter")
			h.typeText("ORD-2")
			h.press("ctrl+s")
			h.typeText("y")

			Expect(h.r.started).To(HaveLen(n))
			Expect(h.view()).To(ContainSubstring("Send Unlock an order to staging?"))
		})

		It("says a read-only project will refuse it, and still leaves the refusal to fft", func() {
			h = newHarness(Options{ReadOnly: true})
			h.loaded(twoProjects, validToken)
			h.request(opUnlockOrder)
			h.fill(0, "ORD-1")
			Expect(h.view()).To(ContainSubstring("This project or session is read-only"))

			h.press("s")
			Expect(h.view()).To(ContainSubstring("fft will refuse it and send"))
			h.wait()
			h.press("y")
			Expect(h.last().Args).To(Equal([]string{"order", "unlock", "ORD-1"}))
		})

		It("puts an argument that starts with a dash after every flag", func() {
			h.request(opAddPickJob)
			h.m.request.body = []byte(`{}`)
			h.request(opUnlockOrder)
			h.fill(0, "-odd")
			h.press("s")
			h.wait()
			h.press("y")

			Expect(h.last().Args).To(Equal([]string{"order", "unlock", "--", "-odd"}))
		})
	})

	Describe("a write whose command asks its own question", func() {
		var run RunID

		BeforeEach(func() {
			h.request(opDeleteFacility)
			h.fill(0, "BER-01")
			h.press("s")
			run = RunID(len(h.r.started))
		})

		It("is sent at once, without --yes, for the command to ask", func() {
			Expect(h.last().Args).To(Equal([]string{"facility", "delete", "BER-01"}))
			Expect(h.last().Project).To(Equal("staging"))
			Expect(h.m.current).To(Equal(tabResponse))
			Expect(h.m.owner()).To(Equal(ownerScreen))
		})

		It("says on the form that the command asks", func() {
			h.request(opDeleteFacility)
			Expect(h.view()).To(ContainSubstring("the command asks first, once it has looked up what it changes"))
		})

		It("shows the command's question, whatever screen is in front", func() {
			h.press("2")
			h.ask(run, "Delete facility Berlin Mitte (BER-01)? This cannot be undone.", "delete")

			Expect(h.m.owner()).To(Equal(ownerQuestion))
			view := h.view()
			Expect(view).To(ContainSubstring("Delete facility Berlin Mitte (BER-01)? This cannot be undone."))
			Expect(view).To(ContainSubstring("Command #%d is waiting for your answer, on project staging.", run))
			Expect(view).To(ContainSubstring("Type delete to confirm."))
			Expect(view).To(ContainSubstring("runs: fft facility delete BER-01 --project staging"))
			Expect(view).To(ContainSubstring("enter confirm · esc cancel"))
			Expect(view).NotTo(ContainSubstring("$ fft"), "a question offers no command to copy")
		})

		It("answers yes only once the word is typed back", func() {
			q := h.ask(run, "Delete facility BER-01? This cannot be undone.", "delete")
			h.wait()
			h.typeText("y")
			h.press("enter")
			Expect(h.r.answers).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("That is not the word; nothing was sent."))

			h.press("x")
			h.m.s.asking().dialog.dialog.(*typeNameDialog).input.SetValue("")
			h.typeText("delete")
			h.press("enter")
			Expect(h.r.answers).To(Equal([]answer{{run: run, question: q, yes: true}}))
			Expect(h.m.owner()).To(Equal(ownerScreen))
			Expect(h.m.s.runs.byID[run].asking).To(BeFalse())
		})

		It("answers no on esc", func() {
			q := h.ask(run, "Delete facility BER-01?", "delete")
			h.wait()
			h.press("esc")

			Expect(h.r.answers).To(Equal([]answer{{run: run, question: q, yes: false}}))
		})

		It("takes no key before it has been in front of the user for a moment", func() {
			h.ask(run, "Delete facility BER-01?", "delete")
			h.typeText("delete")
			h.press("enter")

			Expect(h.r.answers).To(BeEmpty())
			Expect(h.m.s.asking().dialog.dialog.(*typeNameDialog).input.Value()).To(BeEmpty())
		})

		It("takes a y for a question that does not ask for a word, and not a pasted one", func() {
			q := h.ask(run, "Replace the template?", "")
			h.wait()
			h.send(tea.PasteMsg{Content: "y"})
			Expect(h.r.answers).To(BeEmpty())

			h.press("y")
			Expect(h.r.answers).To(Equal([]answer{{run: run, question: q, yes: true}}))
		})

		It("waits behind a field being typed into, says so, and is armed only once it shows", func() {
			h.request(opGetPickJob)
			h.press("enter")
			h.ask(run, "Delete facility BER-01?", "")

			Expect(h.m.owner()).To(Equal(ownerFocused))
			Expect(h.view()).To(ContainSubstring("1 waiting for your answer"))
			h.typeText("pj-y")
			Expect(h.r.answers).To(BeEmpty())
			Expect(h.m.request.fields[0].value()).To(Equal("pj-y"))

			h.wait()
			h.press("enter")
			Expect(h.m.owner()).To(Equal(ownerQuestion))
			h.press("y")
			Expect(h.r.answers).To(BeEmpty(), "a y pressed as the question appeared answered it")
		})

		It("waits behind the command panel, which shows the run waiting", func() {
			h.press("i")
			h.ask(run, "Delete facility BER-01?", "delete")

			Expect(h.m.owner()).To(Equal(ownerPanel))
			Expect(h.view()).To(MatchRegexp(`#%d\s+fft facility delete BER-01 --project staging\s+waiting for your answer`, run))
			Expect(h.view()).To(ContainSubstring("It is waiting for your answer to its question."))

			h.press("esc")
			Expect(h.m.owner()).To(Equal(ownerQuestion))
		})

		It("lets the panel cancel a run that is waiting", func() {
			h.press("i")
			h.ask(run, "Delete facility BER-01?", "delete")
			h.press("c")

			Expect(h.r.cancelled).To(Equal([]RunID{run}))
			h.finishID(run, Result{ExitCode: exitcode.Interrupted})
			h.press("esc")
			Expect(h.m.owner()).To(Equal(ownerScreen))
			Expect(h.r.answers).To(BeEmpty(), "the runner answers a cancelled question itself")
		})

		It("asks one question at a time, in the order they came, and answers each for its own run", func() {
			h.request(opDeleteFacility)
			h.fill(0, "HAM-02")
			h.press("s")
			second := RunID(len(h.r.started))

			q1 := h.ask(run, "Delete facility BER-01?", "")
			q2 := h.ask(second, "Delete facility HAM-02?", "")
			Expect(h.view()).To(ContainSubstring("Delete facility BER-01?"))
			Expect(h.view()).To(ContainSubstring("2 waiting for your answer"))

			h.wait()
			h.press("n")
			Expect(h.view()).To(ContainSubstring("Delete facility HAM-02?"))
			h.press("y")
			Expect(h.r.answers).To(HaveLen(1), "a y pressed as the second question appeared answered it")
			h.wait()
			h.press("y")

			Expect(h.r.answers).To(Equal([]answer{
				{run: run, question: q1, yes: false},
				{run: second, question: q2, yes: true},
			}))
		})

		It("drops the question of a run that has ended", func() {
			h.ask(run, "Delete facility BER-01?", "delete")
			h.finishID(run, Result{ExitCode: exitcode.Interrupted})

			Expect(h.m.owner()).To(Equal(ownerScreen))
			Expect(h.m.s.questions).To(BeEmpty())
			Expect(h.r.answers).To(BeEmpty())
		})

		It("answers no for a run it did not start", func() {
			q := h.ask(99, "Delete everything?", "")

			Expect(h.m.owner()).To(Equal(ownerScreen))
			Expect(h.r.answers).To(Equal([]answer{{run: 99, question: q, yes: false}}))
		})

		It("draws nothing of the question a terminal would act on", func() {
			h.ask(run, "Delete \x1b]0;owned\x07facility \x1b[2Jnow?", "delete")

			content := h.m.View().Content
			Expect(content).NotTo(ContainSubstring("\x1b]0;"))
			Expect(content).NotTo(ContainSubstring("\x1b[2J"))
			Expect(h.view()).To(ContainSubstring("facility"))
		})

		It("quits on ctrl+c, as every dialog does", func() {
			h.ask(run, "Delete facility BER-01?", "delete")
			h.press("ctrl+c")
			Expect(h.m.confirmQuit).To(BeTrue())
		})
	})

	Describe("the body", func() {
		BeforeEach(func() {
			h.request(opAddPickJob)
		})

		It("is required before the request goes", func() {
			n := len(h.r.started)
			h.press("s")

			Expect(h.r.started).To(HaveLen(n))
			Expect(h.view()).To(ContainSubstring("the body is required: press e to write it"))
		})

		It("is edited in the user's editor, on a private file seeded with the sample", func() {
			h.press("e")

			cmd := h.editor.cmds[0]
			Expect(cmd.Args[:2]).To(Equal([]string{"fake-editor", "--wait"}))
			path := h.editor.path()
			dir := filepath.Dir(path)
			Expect(filepath.Dir(dir)).To(Equal(h.tmp))
			Expect(filepath.Base(path)).To(Equal("body.json"))
			Expect(os.ReadFile(path)).To(BeEquivalentTo(opAddPickJob.SampleBody))

			if runtime.GOOS != "windows" {
				info, err := os.Stat(path)
				Expect(err).NotTo(HaveOccurred())
				Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))

				info, err = os.Stat(dir)
				Expect(err).NotTo(HaveOccurred())
				Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)), "the directory is the session's alone")
			}
		})

		It("gives the editor none of fft's credentials", func() {
			h.env["FFT_PASSWORD"] = "hunter2"
			h.env["fft_api_key"] = "AIzaSyExample"
			h.env["PATH"] = "/usr/bin"
			h.press("e")

			env := h.editor.cmds[0].Env
			Expect(env).NotTo(BeNil(), "a nil Env inherits everything")
			Expect(env).To(ContainElements("PATH=/usr/bin", "EDITOR=fake-editor --wait"))
			Expect(env).NotTo(ContainElement(ContainSubstring("hunter2")))
			Expect(env).NotTo(ContainElement(ContainSubstring("AIzaSyExample")))
		})

		It("is shown, from its start, in the question asked before a write sends it", func() {
			const edited = `{"pickLineItems":[{"article":"A-1"}]}`
			h.press("e")
			h.editorExits([]byte(edited), nil)
			h.press("s")

			Expect(h.view()).To(ContainSubstring("The body, 37 bytes:"))
			Expect(h.view()).To(ContainSubstring(`"article": "A-1"`))
		})

		It("travels on stdin, never on the command line, and the file is removed", func() {
			const edited = `{"pickLineItems":[{"article":"stdin-only"}]}`
			h.press("e")
			h.editorExits([]byte(edited), nil)

			Expect(h.files()).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("Body: 44 bytes, sent on stdin"))
			Expect(h.view()).To(ContainSubstring("The body is ready to send."))

			h.press("s")
			h.wait()
			h.press("y")
			inv := h.last()
			Expect(inv.Args).To(Equal([]string{"picking", "add-pick-job", "--file", "-"}))
			Expect(string(inv.Stdin)).To(Equal(edited))
			Expect(strings.Join(inv.Args, " ")).NotTo(ContainSubstring("stdin-only"))
			Expect(h.view()).NotTo(ContainSubstring("stdin-only"))
		})

		It("removes what the editor left beside the body, with the body", func() {
			h.press("e")
			dir := filepath.Dir(h.editor.path())
			Expect(os.WriteFile(filepath.Join(dir, ".body.json.swp"), []byte(`{"secret":1}`), 0o600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "body.json~"), []byte(`{"secret":1}`), 0o600)).To(Succeed())
			h.editorExits([]byte(`{"a":1}`), nil)

			Expect(h.files()).To(BeEmpty())
			Expect(dir).NotTo(BeADirectory())
		})

		It("reopens the body as it was left", func() {
			h.press("e")
			h.editorExits([]byte(`{"a":1}`), nil)
			h.press("e")

			Expect(os.ReadFile(h.editor.path())).To(BeEquivalentTo(`{"a":1}`))
		})

		It("keeps the body, and removes the file, when the editor fails", func() {
			h.press("e")
			h.editorExits([]byte(`{"a":1}`), nil)
			h.press("e")
			h.editorExits([]byte(`{"lost":true}`), errors.New("exit status 1"))

			Expect(h.files()).To(BeEmpty())
			Expect(string(h.m.request.body)).To(Equal(`{"a":1}`))
			Expect(h.view()).To(ContainSubstring("editing the body failed"))
			Expect(h.view()).To(ContainSubstring("exit status 1"))
		})

		It("keeps a body that is not JSON for fixing, and refuses to send it", func() {
			h.press("e")
			h.editorExits([]byte(`{"a":`), nil)

			n := len(h.r.started)
			h.press("s")
			Expect(h.r.started).To(HaveLen(n))
			Expect(h.view()).To(ContainSubstring("the body is not valid JSON"))
		})

		It("sends none when the editor leaves the file empty", func() {
			h.press("e")
			h.editorExits([]byte("  \n"), nil)

			Expect(h.m.request.body).To(BeNil())
			Expect(h.view()).To(ContainSubstring("The body is empty, so none will be sent."))
		})

		It("opens nothing a second time while the editor is open", func() {
			h.press("e")
			h.press("e")
			Expect(h.editor.cmds).To(HaveLen(1))
		})

		It("says a command without a body has none to edit", func() {
			h.request(opGetPickJob)
			h.press("e")

			Expect(h.editor.cmds).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("takes no request body"))
		})

		It("leaves no file behind when the editor cannot be named", func() {
			h.env["EDITOR"] = `vim "unclosed`
			h.press("e")

			Expect(h.editor.cmds).To(BeEmpty())
			Expect(h.files()).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("$EDITOR: a quote or a backslash is not closed"))
		})

		It("removes the directory of an editor still open when the UI ends", func() {
			h.press("e")
			Expect(h.files()).To(HaveLen(1))
			swap := filepath.Join(filepath.Dir(h.editor.path()), ".body.json.swp")
			Expect(os.WriteFile(swap, []byte(`{}`), 0o600)).To(Succeed())

			h.m.s.cleanup()
			Expect(h.files()).To(BeEmpty())
		})
	})

	Describe("a command that prints an example body of its own", func() {
		BeforeEach(func() {
			h.request(opReplaceFacility)
			h.fill(0, "BER-01")
			h.fill(1, "MANAGED")
			h.fill(2, "3")
			h.press("e")
		})

		It("asks the command for it, with the flags that shape it", func() {
			Expect(h.editor.cmds).To(BeEmpty())
			id := h.lookup("facility", "update", "--kind", "MANAGED", "--example")
			Expect(h.r.invocation(id).Stdin).To(BeNil())
			Expect(h.view()).To(ContainSubstring("Fetching the example body"))
		})

		It("opens the editor on what the command printed", func() {
			h.finish(ok(`{"name":"from the command"}`), "facility", "update", "--kind", "MANAGED", "--example")

			Expect(os.ReadFile(h.editor.path())).To(BeEquivalentTo(`{"name":"from the command"}`))
		})

		It("falls back to the operation's sample when the command prints none", func() {
			h.finish(failed(exitcode.Usage, "no example"), "facility", "update", "--kind", "MANAGED", "--example")

			Expect(os.ReadFile(h.editor.path())).To(BeEquivalentTo(opReplaceFacility.SampleBody))
			Expect(h.view()).To(ContainSubstring("this is the operation's sample body"))
		})

		It("does not open the editor over a screen the user has since gone to", func() {
			h.press("1")
			h.finish(ok(`{"name":"x"}`), "facility", "update", "--kind", "MANAGED", "--example")

			Expect(h.editor.cmds).To(BeEmpty())
			h.press("3")
			Expect(h.view()).To(ContainSubstring("The example body is ready: press e to edit it."))

			h.press("e")
			Expect(os.ReadFile(h.editor.path())).To(BeEquivalentTo(`{"name":"x"}`))
			Expect(h.r.commandLines()).To(HaveLen(3), "the example was asked for more than once")
		})

		It("ignores the answer once another form has been opened", func() {
			h.request(opAddPickJob)
			h.finish(ok(`{"name":"x"}`), "facility", "update", "--kind", "MANAGED", "--example")

			Expect(h.editor.cmds).To(BeEmpty())
		})
	})
})

var _ = DescribeTable("splitting name=value pairs",
	func(value string, want []string) {
		Expect(splitPairs(value)).To(Equal(want))
	},
	Entry("one pair", "status=OPEN", []string{"status=OPEN"}),
	Entry("a list value", "status=OPEN,CLOSED", []string{"status=OPEN,CLOSED"}),
	Entry("two pairs", "status=OPEN, size=5", []string{"status=OPEN", " size=5"}),
	Entry("a list, then a pair", "status=OPEN,CLOSED,size=5", []string{"status=OPEN,CLOSED", "size=5"}),
	Entry("a header value with commas and equals signs", "Accept=text/html;q=0.9,application/json",
		[]string{"Accept=text/html;q=0.9,application/json"}),
	Entry("a value that is only commas", "tags=,,", []string{"tags=,,"}),
	Entry("a bracketed name", "a=1,filter[x]=2", []string{"a=1", "filter[x]=2"}),
	Entry("a comma before something with a space in its name", "a=1,b c=2", []string{"a=1,b c=2"}),
	Entry("no pair at all", "", []string{""}),
)

var _ = DescribeTable("splitting an editor setting into words",
	func(value string, backslashes bool, want []string) {
		Expect(splitCommand(value, backslashes)).To(Equal(want))
	},
	Entry("a program", "vim", true, []string{"vim"}),
	Entry("a program and its flags", "code --wait  -n", true, []string{"code", "--wait", "-n"}),
	Entry("a path with a space, quoted", `"/Applications/Sublime Text.app/subl" -w`, true,
		[]string{"/Applications/Sublime Text.app/subl", "-w"}),
	Entry("single quotes, kept literally", `'a\b "c"'`, true, []string{`a\b "c"`}),
	Entry("an escaped space", `my\ editor`, true, []string{"my editor"}),
	Entry("an escaped quote in double quotes", `"say \"hi\""`, true, []string{`say "hi"`}),
	Entry("a backslash in double quotes that escapes nothing", `"a\b"`, true, []string{`a\b`}),
	Entry("shell syntax, which is only text", `vim $(rm -rf x); echo`, true, []string{"vim", "$(rm", "-rf", "x);", "echo"}),
	Entry("a Windows path", `C:\Tools\edit.exe /n`, false, []string{`C:\Tools\edit.exe`, "/n"}),
	Entry("an empty quoted word", `ed ''`, true, []string{"ed", ""}),
)

var _ = DescribeTable("refusing an editor setting that does not end",
	func(value string) {
		_, err := splitCommand(value, true)
		Expect(err).To(MatchError(errUnclosed))
	},
	Entry("an open double quote", `vim "file`),
	Entry("an open single quote", `vim 'file`),
	Entry("a trailing backslash", `vim \`),
)

var _ = Describe("choosing the editor", func() {
	env := func(vars map[string]string) func(string) string {
		return func(name string) string { return vars[name] }
	}

	It("takes $VISUAL first", func() {
		Expect(editorCommand(env(map[string]string{"EDITOR": "nano", "VISUAL": "code -w"}))).To(Equal([]string{"code", "-w"}))
	})

	It("takes $EDITOR when $VISUAL is not set", func() {
		Expect(editorCommand(env(map[string]string{"EDITOR": "nano", "VISUAL": "  "}))).To(Equal([]string{"nano"}))
	})

	It("falls back to one the system has", func() {
		Expect(editorCommand(env(nil))).To(HaveLen(1))
	})
})
