package tui

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeRunner records what the UI asks of it and runs nothing. A spec finishes a
// run by handing the model the event the real runner would have sent.
type fakeRunner struct {
	started   []Invocation
	cancelled []RunID
	answers   []answer
	projects  []string
	events    chan RunEvent
	startErr  error
}

// answer is one call to [Runner.Answer].
type answer struct {
	run      RunID
	question uint64
	yes      bool
}

func newFakeRunner() *fakeRunner {
	r := &fakeRunner{events: make(chan RunEvent)}
	// Anything still waiting on an event gives up once the spec is over.
	DeferCleanup(func() { close(r.events) })
	return r
}

func (r *fakeRunner) Start(inv Invocation) (RunID, error) {
	if r.startErr != nil {
		return 0, r.startErr
	}
	r.started = append(r.started, inv)
	return RunID(len(r.started)), nil
}

func (r *fakeRunner) Cancel(id RunID)         { r.cancelled = append(r.cancelled, id) }
func (r *fakeRunner) Events() <-chan RunEvent { return r.events }
func (r *fakeRunner) Answer(id RunID, q uint64, yes bool) {
	r.answers = append(r.answers, answer{run: id, question: q, yes: yes})
}
func (r *fakeRunner) SetProject(name string)         { r.projects = append(r.projects, name) }
func (r *fakeRunner) args(id RunID) []string         { return r.started[id-1].Args }
func (r *fakeRunner) stdin(id RunID) string          { return string(r.started[id-1].Stdin) }
func (r *fakeRunner) exclusive(id RunID) bool        { return r.started[id-1].Exclusive }
func (r *fakeRunner) invocation(id RunID) Invocation { return r.started[id-1] }

// commandLines is every started invocation's argv, joined, in order.
func (r *fakeRunner) commandLines() []string {
	lines := make([]string, len(r.started))
	for i, inv := range r.started {
		lines[i] = strings.Join(inv.Args, " ")
	}
	return lines
}

// harness drives the root model the way Bubble Tea would, one message at a time.
type harness struct {
	r      *fakeRunner
	m      *app
	now    time.Time
	done   map[RunID]bool
	editor *fakeEditor
	env    map[string]string
	tmp    string

	// questions counts the questions the harness has had runs ask.
	questions uint64
}

func newHarness(opts Options) *harness {
	h := &harness{
		r:      newFakeRunner(),
		now:    time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC),
		done:   map[RunID]bool{},
		editor: &fakeEditor{},
		env:    map[string]string{"EDITOR": "fake-editor --wait"},
		tmp:    GinkgoT().TempDir(),
	}
	opts.Runner = h.r
	opts.Now = func() time.Time { return h.now }
	if opts.Catalog == nil {
		opts.Catalog = fakeCatalog{}
	}
	opts.execProcess = h.editor.exec
	opts.getenv = func(name string) string { return h.env[name] }
	opts.tempDir = h.tmp
	h.m = newApp(opts)
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	// Init's own command waits on the runner; only its side effects matter here.
	_ = h.m.Init()
	return h
}

func (h *harness) send(msg tea.Msg) tea.Cmd {
	_, cmd := h.m.Update(msg)
	return cmd
}

// press sends each key in turn, and returns what the last one asked for.
func (h *harness) press(keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		cmd = h.send(keyPress(k))
	}
	return cmd
}

// typeText types s one character at a time.
func (h *harness) typeText(s string) {
	for _, r := range s {
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func keyPress(k string) tea.KeyPressMsg {
	special := map[string]tea.KeyPressMsg{
		"enter":     {Code: tea.KeyEnter},
		"esc":       {Code: tea.KeyEscape},
		"tab":       {Code: tea.KeyTab},
		"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
		"up":        {Code: tea.KeyUp},
		"down":      {Code: tea.KeyDown},
		"space":     {Code: tea.KeySpace, Text: " "},
		"ctrl+c":    {Code: 'c', Mod: tea.ModCtrl},
		"ctrl+p":    {Code: 'p', Mod: tea.ModCtrl},
		"ctrl+r":    {Code: 'r', Mod: tea.ModCtrl},
		"ctrl+s":    {Code: 's', Mod: tea.ModCtrl},
		"left":      {Code: tea.KeyLeft},
		"right":     {Code: tea.KeyRight},
	}
	if msg, ok := special[k]; ok {
		return msg
	}
	r := []rune(k)
	Expect(r).To(HaveLen(1), "not a key the harness knows: %q", k)
	return tea.KeyPressMsg{Code: r[0], Text: k}
}

// lookup is the most recent run started with exactly args.
func (h *harness) lookup(args ...string) RunID {
	GinkgoHelper()
	want := strings.Join(args, " ")
	for i := len(h.r.started); i > 0; i-- {
		if strings.Join(h.r.started[i-1].Args, " ") == want && !h.done[RunID(i)] {
			return RunID(i)
		}
	}
	Fail("no unfinished run of: " + want + "\nstarted: " + strings.Join(h.r.commandLines(), " | "))
	return 0
}

// finish ends the run started with args, as the runner would report it.
func (h *harness) finish(res Result, args ...string) tea.Cmd {
	GinkgoHelper()
	return h.finishID(h.lookup(args...), res)
}

func (h *harness) finishID(id RunID, res Result) tea.Cmd {
	h.done[id] = true
	return h.send(runEventMsg{ID: id, State: RunDone, Invocation: h.r.invocation(id), At: h.now, Result: res})
}

// wait lets the clock run past the moment a dialog starts taking keys.
func (h *harness) wait() {
	h.now = h.now.Add(armDelay)
}

// ask has run id ask text, as the runner reports a command's question, and returns
// the question's id.
func (h *harness) ask(id RunID, text, confirm string) uint64 {
	h.questions++
	inv := Invocation{}
	if int(id) <= len(h.r.started) {
		inv = h.r.invocation(id)
	}
	h.send(runEventMsg{ID: id, State: RunRunning, Invocation: inv, At: h.now,
		Question: &Question{ID: h.questions, Text: text, Confirm: confirm}})
	return h.questions
}

func ok(stdout string) Result { return Result{ExitCode: 0, Stdout: []byte(stdout)} }

func failed(code int, stderr string) Result {
	return Result{ExitCode: code, Stderr: []byte(stderr)}
}

// view is the screen as a user reads it, without the escape sequences.
func (h *harness) view() string {
	return ansi.Strip(h.m.View().Content)
}

// msgsOf runs cmd and whatever it batches, and returns the messages that arrive
// promptly. A command that waits — on the runner, on a timer — is left waiting.
func msgsOf(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		if batch, isBatch := msg.(tea.BatchMsg); isBatch {
			var all []tea.Msg
			for _, c := range batch {
				all = append(all, msgsOf(c)...)
			}
			return all
		}
		return []tea.Msg{msg}
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

func quits(cmd tea.Cmd) bool {
	return slices.ContainsFunc(msgsOf(cmd), func(m tea.Msg) bool {
		_, isQuit := m.(tea.QuitMsg)
		return isQuit
	})
}

// clipboard is what cmd put on the clipboard, "" if nothing. Bubble Tea's message
// for it is unexported, and is a string underneath.
func clipboard(cmd tea.Cmd) string {
	for _, m := range msgsOf(cmd) {
		v := reflect.ValueOf(m)
		if v.Kind() == reflect.String && strings.Contains(v.Type().String(), "setClipboard") {
			return v.String()
		}
	}
	return ""
}

var errShutDown = errors.New("the command runner has been shut down")

const (
	twoProjects = `[
	  {"name":"staging","active":true,"baseUrl":"https://staging.example.com","email":"bot@ocff-acme-staging.com","credential":"keyring","readOnly":false},
	  {"name":"prod","active":false,"baseUrl":"https://prod.example.com","email":"bot@ocff-acme-prd.com","credential":"keyring","readOnly":true}
	]`

	validToken = `{"project":"staging","store":"keyring","signIn":"password","token":"valid",
	  "expired":false,"expiresAt":"2026-07-12T12:42:00Z"}`
)

// loaded has the harness answer the two runs the UI starts with.
func (h *harness) loaded(projects, status string) {
	GinkgoHelper()
	h.finish(ok(projects), "project", "list")
	h.finish(ok(status), "auth", "status")
}

// pump hands the model every message cmd produces, and what those produce in turn,
// the way Bubble Tea would: how the list's search results come back.
func (h *harness) pump(cmd tea.Cmd) {
	for range 4 {
		msgs := msgsOf(cmd)
		if len(msgs) == 0 {
			return
		}
		var next []tea.Cmd
		for _, msg := range msgs {
			next = append(next, h.send(msg))
		}
		cmd = tea.Batch(next...)
	}
}

// search types query into the operations list's search, and applies it.
func (h *harness) search(query string) {
	GinkgoHelper()
	h.pump(h.press("/"))
	for _, r := range query {
		h.pump(h.send(tea.KeyPressMsg{Code: r, Text: string(r)}))
	}
}

// files is every file in the harness's temporary directory.
func (h *harness) files() []string {
	GinkgoHelper()
	entries, err := os.ReadDir(h.tmp)
	Expect(err).NotTo(HaveOccurred())
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// fakeEditor stands in for tea.ExecProcess: it records the editor the UI would
// hand the terminal to, and runs nothing until a spec says how the editor exits.
type fakeEditor struct {
	cmds      []*exec.Cmd
	callbacks []tea.ExecCallback
}

// editorOpened is what the fake's command produces; the real one hands the
// terminal over instead.
type editorOpened struct{}

func (e *fakeEditor) exec(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	e.cmds = append(e.cmds, c)
	e.callbacks = append(e.callbacks, fn)
	return func() tea.Msg { return editorOpened{} }
}

// path is the file the last editor was opened on.
func (e *fakeEditor) path() string {
	GinkgoHelper()
	Expect(e.cmds).NotTo(BeEmpty(), "no editor was opened")
	args := e.cmds[len(e.cmds)-1].Args
	return args[len(args)-1]
}

// exit has the last editor save content, unless it is nil, and exit with err.
func (h *harness) editorExits(content []byte, err error) tea.Cmd {
	GinkgoHelper()
	path := h.editor.path()
	if content != nil {
		Expect(os.WriteFile(path, content, 0o600)).To(Succeed())
	}
	fn := h.editor.callbacks[len(h.editor.callbacks)-1]
	return h.send(fn(err))
}

// The operations the fake catalog lists.
var (
	opListFacilities = Operation{
		ID: "searchFacility", Summary: "Search facilities", Method: "POST", Path: "/api/facilities/search",
		Tag: "Facilities (Core)", Permissions: []string{"FACILITY_READ"},
		Command: Command{
			Path: []string{"facility", "list"}, Curated: true, Table: true,
			Flags: []Flag{
				{Name: "status", Kind: FlagList, Usage: "Only facilities in this status", Enum: []string{"ONLINE", "OFFLINE"}},
				{Name: "size", Kind: FlagInt, Usage: "Page size", Default: "20"},
				{Name: "all", Kind: FlagBool, Usage: "Every page"},
			},
		},
	}
	opDeleteFacility = Operation{
		ID: "deleteFacility", Summary: "Delete a facility", Method: "DELETE", Path: "/api/facilities/{facilityId}",
		Tag: "Facilities (Core)", Mutates: true, Permissions: []string{"FACILITY_WRITE"},
		Command: Command{
			Path: []string{"facility", "delete"}, Curated: true, Confirms: true,
			Args: []Arg{{Name: "id", Required: true}},
		},
	}
	opReplaceFacility = Operation{
		ID: "replaceFacility", Summary: "Replace a facility", Method: "PUT", Path: "/api/facilities/{facilityId}",
		Tag: "Facilities (Core)", Mutates: true, SampleBody: `{"name":"sample"}`,
		Command: Command{
			Path: []string{"facility", "update"}, Curated: true, Body: true, BodyRequired: true, Example: true,
			Args: []Arg{{Name: "id", Required: true}},
			Flags: []Flag{
				{Name: "kind", Kind: FlagString, WithExample: true},
				{Name: "if-version", Kind: FlagInt},
			},
		},
	}
	opAddPickJob = Operation{
		ID: "addPickJob", Summary: "Create a pick job", Method: "POST", Path: "/api/pickjobs",
		Tag: "Picking (Operations)", Mutates: true, SampleBody: `{"pickLineItems":[]}`,
		Description: "Creates a pick job for the given line items.",
		Command:     Command{Path: []string{"picking", "add-pick-job"}, Body: true, BodyRequired: true},
	}
	opUnlockOrder = Operation{
		ID: "orderAction", Summary: "Unlock an order", Method: "POST", Path: "/api/orders/{orderId}/actions",
		Tag: "Orders (Operations)", Mutates: true,
		Command: Command{
			Path: []string{"order", "unlock"}, Curated: true,
			Args: []Arg{{Name: "id", Required: true}},
		},
	}
	opAPIGetPickJobs = Operation{
		ID: "getPickJobs", Summary: "List pick jobs", Method: "GET", Path: "/api/pickjobs",
		Tag: "Picking (Operations)",
		Command: Command{
			Path: []string{"api", "getPickJobs"},
			Flags: []Flag{
				{Name: "header", Kind: FlagPairs},
				{Name: "param", Kind: FlagPairs},
				{Name: "query", Kind: FlagPairs},
			},
		},
	}
	opGetPickJob = Operation{
		ID: "getPickJob", Summary: "Get a pick job", Method: "GET", Path: "/api/pickjobs/{pickJobId}",
		Tag: "Picking (Operations)",
		Command: Command{
			Path:  []string{"picking", "get-pick-job"},
			Flags: []Flag{{Name: "pick-job-id", Kind: FlagString, Required: true, Usage: "The pick job"}},
		},
	}
)

// fakeCatalog lists a handful of operations, and draws a table by quoting what it
// was given.
type fakeCatalog struct{}

func (fakeCatalog) Groups() []OperationGroup {
	return []OperationGroup{
		{Tag: "Facilities (Core)", Operations: []Operation{opDeleteFacility, opReplaceFacility, opListFacilities}},
		{Tag: "Picking (Operations)", Operations: []Operation{opAddPickJob, opGetPickJob}},
	}
}

func (fakeCatalog) Table(cmd Command, stdout []byte) (string, error) {
	if !cmd.Table {
		return "", errors.New("no table")
	}
	return "TABLE OF " + string(stdout), nil
}
