package tui

import (
	"errors"
	"os"
	"path/filepath"
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

		It("joins a value that starts with a dash to its flag", func() {
			h.request(opGetPickJob)
			h.fill(0, "-1")
			h.press("s")

			Expect(h.last().Args).To(Equal([]string{"picking", "get-pick-job", "--pick-job-id=-1"}))
		})

		It("leaves the project to the runner, and shows the one it acts on", func() {
			h = newHarness(Options{Project: "staging"})
			h.loaded(twoProjects, validToken)
			h.request(opGetPickJob)
			h.fill(0, "pj-1")
			h.press("s")

			Expect(h.last().Args).NotTo(ContainElement("--project"))
			Expect(h.view()).To(ContainSubstring("fft picking get-pick-job --pick-job-id pj-1 --project staging"))
		})
	})

	Describe("sending a write", func() {
		BeforeEach(func() {
			h.request(opDeleteFacility)
			h.fill(0, "BER-01")
		})

		It("asks first, showing the command a yes runs, and sends nothing yet", func() {
			n := len(h.r.started)
			h.press("s")

			Expect(h.r.started).To(HaveLen(n))
			view := h.view()
			Expect(view).To(ContainSubstring("Send Delete a facility to staging?"))
			Expect(view).To(ContainSubstring("DELETE /api/facilities/{facilityId} changes data on the tenant."))
			Expect(view).To(ContainSubstring("runs: fft facility delete BER-01 --yes"))
			Expect(h.m.owner()).To(Equal(ownerFocused))
		})

		It("sends nothing on no", func() {
			n := len(h.r.started)
			h.press("s", "n")

			Expect(h.r.started).To(HaveLen(n))
			Expect(h.m.current).To(Equal(tabRequest))
		})

		It("sends nothing on a pasted yes", func() {
			n := len(h.r.started)
			h.press("s")
			h.send(tea.PasteMsg{Content: "y"})

			Expect(h.r.started).To(HaveLen(n))
		})

		It("sends it with --yes once confirmed, since the command would ask the same", func() {
			h.press("s", "y")

			Expect(h.last().Args).To(Equal([]string{"facility", "delete", "BER-01", "--yes"}))
			Expect(h.m.current).To(Equal(tabResponse))
		})

		It("adds no --yes to a command that does not ask", func() {
			h.request(opAddPickJob)
			h.m.request.body = []byte(`{"pickLineItems":[]}`)
			h.press("s")
			Expect(h.view()).To(ContainSubstring("runs: fft picking add-pick-job --file -"))
			h.press("y")

			Expect(h.last().Args).To(Equal([]string{"picking", "add-pick-job", "--file", "-"}))
		})

		It("says a read-only project will refuse it, and still leaves the refusal to fft", func() {
			h = newHarness(Options{ReadOnly: true})
			h.loaded(twoProjects, validToken)
			h.request(opDeleteFacility)
			h.fill(0, "BER-01")
			Expect(h.view()).To(ContainSubstring("This project or session is read-only"))

			h.press("s")
			Expect(h.view()).To(ContainSubstring("fft will refuse it and send"))
			h.press("y")
			Expect(h.last().Args).To(Equal([]string{"facility", "delete", "BER-01", "--yes"}))
		})

		It("puts an argument that starts with a dash after every flag", func() {
			h.press("x")
			h.fill(0, "-odd")
			h.press("s", "y")

			Expect(h.last().Args).To(Equal([]string{"facility", "delete", "--yes", "--", "-odd"}))
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
			Expect(filepath.Dir(path)).To(Equal(h.tmp))

			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
			Expect(os.ReadFile(path)).To(BeEquivalentTo(opAddPickJob.SampleBody))
		})

		It("travels on stdin, never on the command line, and the file is removed", func() {
			const edited = `{"pickLineItems":[{"article":"stdin-only"}]}`
			h.press("e")
			h.editorExits([]byte(edited), nil)

			Expect(h.files()).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("Body: 44 bytes"))

			h.press("s", "y")
			inv := h.last()
			Expect(inv.Args).To(Equal([]string{"picking", "add-pick-job", "--file", "-"}))
			Expect(string(inv.Stdin)).To(Equal(edited))
			Expect(strings.Join(inv.Args, " ")).NotTo(ContainSubstring("stdin-only"))
			Expect(h.view()).NotTo(ContainSubstring("stdin-only"))
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

		It("removes the file of an editor still open when the UI ends", func() {
			h.press("e")
			Expect(h.files()).To(HaveLen(1))

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

	It("takes $EDITOR first", func() {
		Expect(editorCommand(env(map[string]string{"EDITOR": "nano", "VISUAL": "code -w"}))).To(Equal([]string{"nano"}))
	})

	It("takes $VISUAL when $EDITOR is not set", func() {
		Expect(editorCommand(env(map[string]string{"EDITOR": "  ", "VISUAL": "code -w"}))).To(Equal([]string{"code", "-w"}))
	})

	It("falls back to one the system has", func() {
		Expect(editorCommand(env(nil))).To(HaveLen(1))
	})
})
