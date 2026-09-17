package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// The operations only the template specs need: a read that takes a body, and a
// write whose command asks its own question.
var (
	opSearchPickJobs = Operation{
		ID: "searchPickJobs", Summary: "Search pick jobs", Method: "POST", Path: "/api/pickjobs/search",
		Tag:     "Picking (Operations)",
		Command: Command{Path: []string{"picking", "search"}, Curated: true, Body: true, BodyRequired: true},
	}
	opPurgeListings = Operation{
		ID: "purgeListings", Summary: "Purge listings", Method: "POST", Path: "/api/listings/purge",
		Tag: "Listings (Core)", Mutates: true,
		Command: Command{Path: []string{"listing", "purge"}, Curated: true, Body: true, Confirms: true},
	}
)

// templateCatalog is the fake catalog, and the operations above.
type templateCatalog struct{ fakeCatalog }

func (c templateCatalog) Groups() []OperationGroup {
	return append(c.fakeCatalog.Groups(),
		OperationGroup{Tag: "Templates", Operations: []Operation{opSearchPickJobs, opPurgeListings}})
}

const (
	// oneTemplate is `template list` with one user template for addPickJob.
	oneTemplate = `[{"name":"rush","scope":"user","operationId":"addPickJob","project":"staging",
	  "params":["email","id"],"description":"Rush pick job","path":"/home/u/.local/share/fft/templates/rush.json"}]`

	// rushDoc is `template show rush`: a required parameter, and one whose default is
	// a string.
	rushDoc = `{
	  "schemaVersion": 1,
	  "description": "Rush pick job",
	  "operationId": "addPickJob",
	  "project": "staging",
	  "params": {
	    "email": {"path": "consumer.email", "required": true, "description": "Who is told"},
	    "id": {"path": "orderRef", "default": "00042"}
	  },
	  "body": {"consumer": {"email": "old@example.de"}, "orderRef": "00042", "big": 9007199254740993}
	}`

	rendered = `{"consumer":{"email":"a@b.de"},"orderRef":"00042","big":9007199254740993}`
)

// templateDoc is rushDoc for another operation, or for none when id is "".
func docFor(id string) string {
	return `{"schemaVersion":1,"operationId":"` + id + `","body":{"a":1}}`
}

// showTemplates goes to the Templates screen and answers its list.
func (h *harness) showTemplates(list string) {
	GinkgoHelper()
	h.press("5")
	h.finish(ok(list), "template", "list")
}

// openTemplate opens the selected template, and answers show with doc.
func (h *harness) openTemplate(doc string) {
	GinkgoHelper()
	h.press("enter")
	h.finish(ok(doc), "template", "show", "rush")
}

// fillParam types value into the template's parameter at row.
func (h *harness) fillParam(row int, value string) {
	GinkgoHelper()
	h.m.templates.open.cursor = row
	h.press("enter")
	Expect(h.m.templates.open.editing).To(BeTrue())
	h.typeText(value)
	h.press("enter")
}

var _ = Describe("the Templates screen", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{Catalog: templateCatalog{}})
		h.loaded(twoProjects, validToken)
		// Wide enough that the command in the status bar is never cut.
		h.send(tea.WindowSizeMsg{Width: 320, Height: 50})
	})

	Describe("the list", func() {
		It("is read the first time the screen is shown, and not before", func() {
			Expect(h.r.commandLines()).NotTo(ContainElement("template list"))

			h.press("5")
			Expect(h.r.commandLines()).To(ContainElement("template list"))
			Expect(h.view()).To(ContainSubstring("Loading templates…"))

			h.finish(ok(oneTemplate), "template", "list")
			h.press("1", "5")
			Expect(strings.Count(strings.Join(h.r.commandLines(), "\n"), "template list")).To(Equal(1))
		})

		It("shows each template with what it is for, and the command that describes it", func() {
			h.showTemplates(oneTemplate)

			Expect(h.view()).To(MatchRegexp(`> rush\s+user\s+addPickJob\s+email, id\s+Rush pick job`))
			Expect(h.view()).To(ContainSubstring("$ fft template show rush"))
		})

		It("says there are none, and how to save one", func() {
			h.showTemplates(`[]`)
			Expect(h.view()).To(ContainSubstring("No templates are saved yet."))
			Expect(h.view()).To(ContainSubstring("with t on the Request screen"))
		})

		It("shows what the list said on stderr about the files it read", func() {
			h.press("5")
			h.finish(Result{Stdout: []byte(oneTemplate), Stderr: []byte(`Hidden by a project template of the same name: "rush".`)},
				"template", "list")
			Expect(h.view()).To(ContainSubstring(`Hidden by a project template of the same name: "rush".`))
		})

		It("says why it could not be read", func() {
			h.press("5")
			h.finish(failed(exitcode.Usage, "Error: no template directory"), "template", "list")
			Expect(h.view()).To(ContainSubstring("listing the templates failed: exit 2"))
			Expect(h.view()).To(ContainSubstring("no template directory"))
		})

		It("is read again with ctrl+r", func() {
			h.showTemplates(oneTemplate)
			h.press("ctrl+r")
			h.lookup("template", "list")
		})

		It("keeps the selected template selected when the list is read again", func() {
			h.showTemplates(`[{"name":"a","scope":"user"},{"name":"rush","scope":"user"}]`)
			h.press("down")
			h.press("ctrl+r")
			h.finish(ok(`[{"name":"a","scope":"user"},{"name":"new","scope":"user"},{"name":"rush","scope":"user"}]`),
				"template", "list")

			Expect(h.view()).To(MatchRegexp(`> rush\s+user`))
			h.press("x")
			h.lookup("template", "remove", "rush")
		})

		It("draws nothing out of a template file that a terminal would act on", func() {
			h.showTemplates("[{\"name\":\"rush\",\"scope\":\"project\"," +
				"\"operationId\":\"addPickJob\\u001b]52;c;cGF5bG9hZA==\\u0007\"," +
				"\"description\":\"\\u001b[2Jevil\\u202e\",\"path\":\"x\"}]")
			Expect(h.view()).To(ContainSubstring("addPickJob]52;c;cGF5bG9hZA=="), "the row is drawn")

			content := h.m.View().Content
			Expect(content).NotTo(ContainSubstring("\x1b]"))
			Expect(content).NotTo(ContainSubstring("\x1b[2J"))
			Expect(content).NotTo(ContainSubstring("\a"))
			Expect(content).NotTo(ContainSubstring("\u202e"))
		})
	})

	Describe("a template's detail", func() {
		BeforeEach(func() {
			h.showTemplates(oneTemplate)
		})

		It("is read with template show, and shows what the template sends and how", func() {
			h.press("enter")
			Expect(h.view()).To(ContainSubstring("Reading the template…"))

			h.finish(ok(rushDoc), "template", "show", "rush")
			view := h.view()
			Expect(view).To(ContainSubstring("rush  user scope"))
			Expect(view).To(ContainSubstring("Rush pick job"))
			Expect(view).To(ContainSubstring("operation  addPickJob  POST /api/pickjobs  →  fft picking add-pick-job  (a write)"))
			Expect(view).To(MatchRegexp(`email\s+consumer.email\s+required`))
			Expect(view).To(MatchRegexp(`id\s+orderRef\s+default "00042"`))
			Expect(view).To(ContainSubstring("Saved body"))
			Expect(view).To(ContainSubstring(`"big": 9007199254740993`), "a 64-bit id keeps its digits")
		})

		It("stands for the pipe S runs", func() {
			h.openTemplate(rushDoc)
			Expect(h.view()).To(ContainSubstring(
				"$ fft template render rush --project staging | fft picking add-pick-job --file - --project staging"))
		})

		It("says when the template was saved under another project", func() {
			h.openTemplate(strings.Replace(rushDoc, `"project": "staging"`, `"project": "prod"`, 1))
			Expect(h.view()).To(ContainSubstring("saved under prod, not staging: ids in the body may not resolve here"))
		})

		It("goes back to the list with esc, and ignores a show that answers after", func() {
			h.press("enter", "esc")
			h.finish(ok(rushDoc), "template", "show", "rush")

			Expect(h.m.templates.open).To(BeNil())
			Expect(h.view()).To(MatchRegexp(`> rush\s+user`))
		})

		It("says why it could not be read", func() {
			h.press("enter")
			h.finish(failed(exitcode.NotFound, "Error: no template rush"), "template", "show", "rush")
			Expect(h.view()).To(ContainSubstring("reading rush failed: exit 6"))
		})
	})

	Describe("the parameters", func() {
		BeforeEach(func() {
			h.showTemplates(oneTemplate)
		})

		It("open with p, once the template has been read, and are typed into", func() {
			h.press("p")
			Expect(h.m.templates.open.form).To(BeFalse(), "nothing to fill in before show answers")
			h.finish(ok(rushDoc), "template", "show", "rush")
			Expect(h.m.templates.open.form).To(BeTrue())

			h.fillParam(0, "a@b.de")
			Expect(h.view()).To(MatchRegexp(`> email\s+consumer.email\s+a@b.de`))
			Expect(h.view()).To(ContainSubstring("Who is told"))
			Expect(h.view()).To(ContainSubstring("$ fft template render rush --set 'email=a@b.de' --project staging |"))
		})

		It("sends a value as a string when the parameter's default is one", func() {
			h.openTemplate(rushDoc)
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.fillParam(1, "12345")
			h.press("R")

			Expect(h.last().Args).To(Equal([]string{
				"template", "render", "rush", "--set", "email=a@b.de", "--set-string", "id=12345",
			}))
		})

		It("keeps every key as text while a value is typed", func() {
			h.openTemplate(rushDoc)
			h.press("p", "enter")
			h.typeText("q5xS")

			Expect(h.m.owner()).To(Equal(ownerFocused))
			Expect(h.m.current).To(Equal(tabTemplates))
			Expect(h.m.confirmQuit).To(BeFalse())
			Expect(h.m.templates.open.params[0].value()).To(Equal("q5xS"))
		})

		It("are remembered when the template is opened again", func() {
			h.openTemplate(rushDoc)
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("esc", "esc")

			h.openTemplate(rushDoc)
			Expect(h.m.templates.open.params[0].value()).To(Equal("a@b.de"))
		})

		It("are not handed to a template of the same name in the other scope", func() {
			h.openTemplate(rushDoc)
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("esc", "esc", "ctrl+r")
			h.finish(ok(strings.Replace(oneTemplate, `"scope":"user"`, `"scope":"project"`, 1)), "template", "list")

			h.openTemplate(rushDoc)
			Expect(h.m.templates.open.params[0].value()).To(BeEmpty())
		})

		It("puts back what a value held when esc leaves it, and clears it with x", func() {
			h.openTemplate(rushDoc)
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("enter")
			h.typeText("zz")
			h.press("esc")
			Expect(h.m.templates.open.params[0].value()).To(Equal("a@b.de"))

			h.press("x")
			Expect(h.m.templates.open.params[0].value()).To(BeEmpty())
		})

		It("never offer a parameter whose name --set would misroute", func() {
			h.openTemplate(`{"schemaVersion":1,"operationId":"addPickJob",` +
				`"params":{"a=b":{"path":"orderRef"},"email":{"path":"consumer.email"}},"body":{"a":1}}`)
			names := []string{}
			for _, f := range h.m.templates.open.params {
				names = append(names, f.name)
			}
			Expect(names).To(Equal([]string{"email"}))
			Expect(h.view()).To(ContainSubstring("Not offered: a=b."))
		})

		It("say so when the template declares none", func() {
			h.openTemplate(docFor("addPickJob"))
			h.press("p")
			Expect(h.m.templates.open.form).To(BeFalse())
			Expect(h.view()).To(ContainSubstring("rush declares no parameters."))
		})
	})

	Describe("rendering", func() {
		BeforeEach(func() {
			h.showTemplates(oneTemplate)
			h.openTemplate(rushDoc)
		})

		It("asks for a required value first, and runs nothing", func() {
			n := len(h.r.started)
			h.press("R")

			Expect(h.r.started).To(HaveLen(n))
			Expect(h.m.templates.open.form).To(BeTrue())
			Expect(h.view()).To(ContainSubstring("Give a value for email first"))
		})

		It("runs template render for the project on screen, and shows the body it printed", func() {
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("R")

			id := h.lookup("template", "render", "rush", "--set", "email=a@b.de")
			Expect(h.r.invocation(id).Project).To(Equal("staging"))
			Expect(h.r.stdin(id)).To(BeEmpty())

			h.finishID(id, ok(rendered))
			Expect(h.view()).To(ContainSubstring("Rendered body"))
			Expect(h.view()).To(ContainSubstring(`"email": "a@b.de"`))
			Expect(h.view()).To(ContainSubstring("Rendered. S sends it; nothing has been sent yet."))
			Expect(h.m.current).To(Equal(tabTemplates))
		})

		It("shows what the render warned about", func() {
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("R")
			h.finish(Result{Stdout: []byte(rendered), Stderr: []byte("Warning: addPickJob is deprecated.\n")},
				"template", "render", "rush", "--set", "email=a@b.de")

			Expect(h.view()).To(ContainSubstring("Warning: addPickJob is deprecated."))
		})

		It("says why it failed", func() {
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("R")
			h.finish(failed(exitcode.Usage, "Error: consumer.email is a string"), "template", "render", "rush", "--set", "email=a@b.de")

			Expect(h.view()).To(ContainSubstring("rendering rush failed: exit 2"))
			Expect(h.view()).To(ContainSubstring("Saved body"))
		})

		It("scrolls the body", func() {
			first := h.m.templates.open.scroll
			h.press("down", "down")
			Expect(h.m.templates.open.scroll).To(Equal(first + 2))
			h.press("up", "up", "up")
			Expect(h.m.templates.open.scroll).To(BeZero())
		})
	})

	Describe("render and send", func() {
		// sendRush renders rush with its required value, and answers the render.
		sendRush := func(res Result) {
			GinkgoHelper()
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("S")
			h.finish(res, "template", "render", "rush", "--set", "email=a@b.de")
		}

		BeforeEach(func() {
			h.showTemplates(oneTemplate)
			h.openTemplate(rushDoc)
		})

		It("hands a write to the Request screen, which asks before it sends the body on stdin", func() {
			sendRush(ok(rendered))

			Expect(h.m.current).To(Equal(tabRequest))
			Expect(h.view()).To(ContainSubstring("Send Create a pick job to staging?"))
			Expect(h.view()).To(ContainSubstring("runs: fft picking add-pick-job --file - --project staging"))
			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("picking")), "nothing is sent before the answer")

			h.press("y")
			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("picking")), "a y at once is not an answer")

			h.wait()
			h.press("y")
			sent := h.last()
			Expect(sent.Args).To(Equal([]string{"picking", "add-pick-job", "--file", "-"}))
			Expect(string(sent.Stdin)).To(Equal(rendered))
			Expect(sent.Project).To(Equal("staging"))
			Expect(h.m.current).To(Equal(tabResponse))
		})

		It("sends nothing when the write is declined", func() {
			sendRush(ok(rendered))
			h.wait()
			h.press("n")

			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("picking")))
			Expect(h.m.request.body).To(Equal([]byte(rendered)), "the body stays in the form")
		})

		It("puts what the render warned about to the user first, and sends nothing on no", func() {
			sendRush(Result{Stdout: []byte(rendered),
				Stderr: []byte("Warning: rush was saved under project \"prod\", and the active project is \"staging\".\n")})

			Expect(h.m.current).To(Equal(tabTemplates))
			Expect(h.view()).To(ContainSubstring("Rendering rush warned. Send it anyway?"))
			Expect(h.view()).To(ContainSubstring(`Warning: rush was saved under project "prod"`))
			Expect(h.m.templates.equivalent().line).To(Equal("fft template render rush --set 'email=a@b.de' --project staging | " +
				"fft picking add-pick-job --file - --project staging"))
			Expect(h.view()).To(ContainSubstring("runs: fft template render rush"))

			h.press("y")
			Expect(h.m.templates.dialog).NotTo(BeNil(), "a y at once is not an answer")

			h.wait()
			h.press("n")
			Expect(h.m.templates.dialog).To(BeNil())
			Expect(h.m.current).To(Equal(tabTemplates))
			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("picking")))
		})

		It("still asks about the write once the warnings are acknowledged", func() {
			sendRush(Result{Stdout: []byte(rendered), Stderr: []byte("Warning: addPickJob is deprecated.\n")})
			h.wait()
			h.press("y")

			Expect(h.m.current).To(Equal(tabRequest))
			Expect(h.view()).To(ContainSubstring("Send Create a pick job to staging?"))
			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("picking")))

			h.wait()
			h.press("y")
			Expect(h.last().Args).To(Equal([]string{"picking", "add-pick-job", "--file", "-"}))
		})

		It("sends a read at once, and shows its response", func() {
			h.press("esc")
			h.openTemplate(docFor("searchPickJobs"))
			h.press("S")
			h.finish(ok(`{"a":1}`), "template", "render", "rush")

			Expect(h.last().Args).To(Equal([]string{"picking", "search", "--file", "-"}))
			Expect(string(h.last().Stdin)).To(Equal(`{"a":1}`))
			Expect(h.m.current).To(Equal(tabResponse))
		})

		It("leaves the question to a command that asks its own, and never adds --yes", func() {
			h.press("esc")
			h.openTemplate(docFor("purgeListings"))
			h.press("S")
			h.finish(ok(`{"a":1}`), "template", "render", "rush")

			id := h.lookup("listing", "purge", "--file", "-")
			Expect(h.m.request.dialog).To(BeNil())

			h.ask(id, "Purge 12 listings?", "purge")
			Expect(h.view()).To(ContainSubstring("Type purge to confirm."))
		})

		It("waits for what the command needs beyond the body, and sends it once given", func() {
			h.press("esc")
			h.openTemplate(docFor("replaceFacility"))
			h.press("S")
			h.finish(ok(`{"a":1}`), "template", "render", "rush")

			Expect(h.m.current).To(Equal(tabRequest))
			Expect(h.view()).To(ContainSubstring("Fill in what the command still needs"))
			Expect(h.view()).To(ContainSubstring("<id> is required"))
			Expect(h.view()).To(ContainSubstring("Body: 7 bytes, sent on stdin"))
			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("facility")))

			h.fill(0, "BER-01")
			h.press("s")
			h.wait()
			h.press("y")
			Expect(h.last().Args).To(Equal([]string{"facility", "update", "BER-01", "--file", "-"}))
			Expect(string(h.last().Stdin)).To(Equal(`{"a":1}`))
		})

		DescribeTable("renders nothing for a template it cannot send",
			func(doc, says string) {
				h.press("esc")
				h.openTemplate(doc)
				n := len(h.r.started)
				h.press("S")

				Expect(h.r.started).To(HaveLen(n))
				Expect(h.view()).To(ContainSubstring(says))
			},
			Entry("no operation", docFor(""), "rush names no operation"),
			Entry("an operation this fft does not know", docFor("goneOperation"), "This fft has no operation goneOperation"),
			Entry("an operation that takes no body", docFor("deleteFacility"), "fft facility delete takes no request body"),
		)

		It("sends nothing when the project changed while the template rendered", func() {
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("S")
			h.m.projects.selectProject("prod")
			h.finish(ok(rendered), "template", "render", "rush", "--set", "email=a@b.de")

			Expect(h.m.current).To(Equal(tabTemplates))
			Expect(h.view()).To(ContainSubstring("The project changed while the template rendered, so nothing was sent."))
			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("picking")))
		})

		It("sends nothing when the project changed while its warnings were read", func() {
			sendRush(Result{Stdout: []byte(rendered), Stderr: []byte("Warning: addPickJob is deprecated.\n")})
			h.m.projects.selectProject("prod")
			h.wait()
			h.press("y")

			Expect(h.m.current).To(Equal(tabTemplates))
			Expect(h.view()).To(ContainSubstring("The project changed before the template was sent"))
			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("picking")))
		})

		It("sends nothing, and takes the user nowhere, once they have moved on", func() {
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("S", "esc", "2")
			h.finish(ok(rendered), "template", "render", "rush", "--set", "email=a@b.de")

			Expect(h.m.current).To(Equal(tabOperations))
			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("picking")))
			h.press("5")
			Expect(h.view()).To(ContainSubstring("nothing was sent: you had moved on"))
		})

		It("leaves a read-only refusal to fft, and says it is coming", func() {
			h.m.s.readOnlyFloor = true
			sendRush(ok(rendered))

			Expect(h.view()).To(ContainSubstring("fft will refuse it and send nothing"))
			h.wait()
			h.press("y")
			Expect(h.last().Args).To(Equal([]string{"picking", "add-pick-job", "--file", "-"}))
		})
	})

	Describe("an answer that arrives while the list is read again", func() {
		It("still hands over a render that a save on the Request screen finished during", func() {
			h.request(opAddPickJob)
			h.m.request.body = []byte(`{"a":1}`)
			h.press("t")
			h.m.request.dialog.(*inputDialog).input.SetValue("other")
			h.press("enter")
			saveID := h.lookup("template", "save", "other", "--operation", "addPickJob", "--file", "-")

			h.showTemplates(oneTemplate)
			h.openTemplate(rushDoc)
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("S")
			renderID := h.lookup("template", "render", "rush", "--set", "email=a@b.de")

			h.finishID(saveID, ok(`{"template":"other","path":"/home/u/.local/share/fft/templates/other.json"}`))
			h.lookup("template", "list")
			h.finishID(renderID, ok(rendered))

			Expect(h.m.current).To(Equal(tabRequest))
			Expect(h.view()).To(ContainSubstring("Send Create a pick job to staging?"))
		})

		It("still shows a template whose show a removal finished during", func() {
			h.showTemplates(`[{"name":"rush","scope":"user"},{"name":"b","scope":"user"}]`)
			h.press("x")
			removeID := h.lookup("template", "remove", "rush")
			h.press("down", "enter")
			showID := h.lookup("template", "show", "b")

			h.finishID(removeID, ok(`{"template":"rush"}`))
			h.finishID(showID, ok(docFor("addPickJob")))

			Expect(h.view()).NotTo(ContainSubstring("Reading the template…"))
			Expect(h.view()).To(ContainSubstring("Saved body"))
		})

		It("still shows a render that a declined removal finished during", func() {
			h.showTemplates(oneTemplate)
			h.openTemplate(rushDoc)
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("R")
			renderID := h.lookup("template", "render", "rush", "--set", "email=a@b.de")
			h.press("esc", "x")
			h.finish(failed(exitcode.Usage, "Error: cancelled"), "template", "remove", "rush")
			h.lookup("template", "list")
			h.finishID(renderID, ok(rendered))

			Expect(h.view()).To(ContainSubstring("Rendered body"))
			Expect(h.view()).NotTo(ContainSubstring("Rendering rush…"))
		})

		It("says nothing more about a render once esc has left its template", func() {
			h.showTemplates(oneTemplate)
			h.openTemplate(rushDoc)
			h.press("p")
			h.fillParam(0, "a@b.de")
			h.press("R", "esc", "esc")
			h.finish(ok(rendered), "template", "render", "rush", "--set", "email=a@b.de")

			Expect(h.m.templates.open).To(BeNil())
			Expect(h.view()).NotTo(ContainSubstring("Rendering rush…"))
		})
	})

	DescribeTable("puts a template name that starts with a dash after every flag",
		func(key string, want ...string) {
			h.showTemplates(`[{"name":"-y","scope":"project","operationId":"addPickJob"}]`)
			if key != "enter" {
				h.press("enter")
				h.finish(ok(docFor("addPickJob")), "template", "show", "--", "-y")
			}
			h.press(key)
			h.lookup(want...)
		},
		Entry("show", "enter", "template", "show", "--", "-y"),
		Entry("render", "R", "template", "render", "--", "-y"),
		Entry("remove", "x", "template", "remove", "--local", "--", "-y"),
	)

	Describe("removing a template", func() {
		It("runs template remove, whose own question is the confirmation, and reads the list again", func() {
			h.showTemplates(oneTemplate)
			h.press("x")

			id := h.lookup("template", "remove", "rush")
			Expect(h.m.templates.dialog).To(BeNil(), "the command asks, not the screen")

			h.ask(id, "Remove the template rush (/home/u/.local/share/fft/templates/rush.json)?", "")
			Expect(h.view()).To(ContainSubstring("Remove the template rush"))
			h.wait()
			h.press("y")
			Expect(h.r.answers).To(ConsistOf(answer{run: id, question: 1, yes: true}))

			h.finishID(id, ok(`{"template":"rush"}`))
			Expect(h.view()).To(ContainSubstring("Removed rush."))
			h.lookup("template", "list")
		})

		It("names the project scope for a project template", func() {
			h.showTemplates(strings.Replace(oneTemplate, `"scope":"user"`, `"scope":"project"`, 1))
			h.press("x")
			h.lookup("template", "remove", "rush", "--local")
		})

		It("leaves a template of the same name in the other scope open", func() {
			h.showTemplates(`[{"name":"rush","scope":"project"},{"name":"b","scope":"user"}]`)
			h.openTemplate(rushDoc)
			h.m.templates.remove(templateRow{Name: "rush", Scope: "user"})
			h.finish(ok(`{"template":"rush"}`), "template", "remove", "rush")

			Expect(h.m.templates.open).NotTo(BeNil())
			Expect(h.m.templates.open.row.Scope).To(Equal("project"))
		})

		It("says nothing was removed when its question is answered no, and that is no failure", func() {
			h.showTemplates(oneTemplate)
			h.openTemplate(rushDoc)
			h.press("x")
			id := h.lookup("template", "remove", "rush")
			h.ask(id, "Remove the template rush?", "")
			h.wait()
			h.press("n")
			Expect(h.r.answers).To(ConsistOf(answer{run: id, question: 1, yes: false}))
			h.finishID(id, failed(exitcode.Usage, "Error: cancelled"))

			Expect(h.view()).To(ContainSubstring("Nothing was removed."))
			Expect(h.view()).NotTo(ContainSubstring("failed"))
			Expect(h.m.templates.open).NotTo(BeNil())
			Expect(h.m.s.declined).To(BeEmpty(), "forgotten once the run has been reported")
		})

		It("says why nothing was removed when the command failed", func() {
			h.showTemplates(oneTemplate)
			h.openTemplate(rushDoc)
			h.press("x")
			h.finish(failed(exitcode.General, "Error: remove: permission denied"), "template", "remove", "rush")

			Expect(h.view()).To(ContainSubstring("removing rush failed: exit 1"))
			Expect(h.view()).To(ContainSubstring("permission denied"))
			Expect(h.m.templates.open).NotTo(BeNil())
		})
	})
})

var _ = Describe("saving a request as a template", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{Catalog: templateCatalog{}})
		h.loaded(twoProjects, validToken)
		h.request(opAddPickJob)
	})

	It("needs a body first", func() {
		h.press("t")
		Expect(h.m.request.dialog).To(BeNil())
		Expect(h.view()).To(ContainSubstring("There is no body to save yet"))
	})

	It("is not offered for a command without a body", func() {
		h.request(opListFacilities)
		Expect(h.view()).NotTo(ContainSubstring("save as template"))
		h.press("t")
		Expect(h.view()).To(ContainSubstring("takes no request body, so there is nothing to save"))
	})

	Describe("with a body", func() {
		BeforeEach(func() {
			h.m.request.body = []byte(`{"pickLineItems":[]}`)
		})

		// saveAs answers the name question with name.
		saveAs := func(name string) {
			GinkgoHelper()
			h.press("t")
			Expect(h.view()).To(ContainSubstring("Save this request's body as a template named:"))
			Expect(h.view()).To(ContainSubstring("not the repository's"))
			in := h.m.request.dialog.(*inputDialog)
			Expect(in.input.Value()).To(Equal("addPickJob"))
			in.input.SetValue(name)
			h.press("enter")
		}

		It("runs template save for the operation, with the body on stdin, for the project on screen", func() {
			saveAs("rush")

			id := h.lookup("template", "save", "rush", "--operation", "addPickJob", "--file", "-")
			Expect(h.r.stdin(id)).To(Equal(`{"pickLineItems":[]}`))
			Expect(h.r.invocation(id).Project).To(Equal("staging"))
			Expect(h.view()).To(ContainSubstring("Saving the template rush…"))

			h.finishID(id, ok(`{"template":"rush"}`))
			Expect(h.view()).To(ContainSubstring("Saved the body as the template rush."))
		})

		It("has the Templates screen read its list again", func() {
			h.press("5")
			h.finish(ok(`[]`), "template", "list")
			h.press("3")

			saveAs("rush")
			h.finish(ok(`{}`), "template", "save", "rush", "--operation", "addPickJob", "--file", "-")
			h.press("5")
			h.lookup("template", "list")
		})

		It("puts a name that starts with a dash after the flags, where fft refuses it as a name", func() {
			saveAs("-rush")
			h.lookup("template", "save", "--operation", "addPickJob", "--file", "-", "--", "-rush")
		})

		It("says why it was not saved", func() {
			saveAs("rush")
			h.finish(failed(exitcode.Usage, `Error: there is already a user template named "rush": pass --force to replace it`),
				"template", "save", "rush", "--operation", "addPickJob", "--file", "-")

			Expect(h.view()).To(ContainSubstring("saving the template rush failed: exit 2"))
			Expect(h.view()).To(ContainSubstring("there is already a user template"))
		})

		It("saves nothing on esc", func() {
			n := len(h.r.started)
			h.press("t", "esc")
			Expect(h.r.started).To(HaveLen(n))
		})

		It("refuses a body that is not JSON", func() {
			h.m.request.body = []byte(`{`)
			h.press("t")
			Expect(h.m.request.dialog).To(BeNil())
			Expect(h.view()).To(ContainSubstring("the body is not valid JSON"))
		})
	})
})
