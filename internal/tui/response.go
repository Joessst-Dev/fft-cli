package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// responseTab is one view of a response.
type responseTab int

const (
	tabJSON responseTab = iota
	tabTable
	tabStderr
)

func (t responseTab) String() string {
	switch t {
	case tabTable:
		return "Table"
	case tabStderr:
		return "Stderr"
	default:
		return "JSON"
	}
}

type responseKeys struct {
	nextTab key.Binding
	prevTab key.Binding
	scroll  key.Binding
	rerun   key.Binding
	save    key.Binding
	cancel  key.Binding
	back    key.Binding
}

func newResponseKeys() responseKeys {
	return responseKeys{
		nextTab: key.NewBinding(key.WithKeys("right"), key.WithHelp("←/→", "view")),
		prevTab: key.NewBinding(key.WithKeys("left")),
		scroll:  key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓/h/l", "scroll")),
		rerun:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "send again")),
		save:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save body")),
		cancel:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cancel")),
		back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "request")),
	}
}

// responseScreen shows one run's outcome: how it ended, what it printed, and what
// it said on stderr.
type responseScreen struct {
	s    *session
	st   styles
	cat  Catalog
	nav  navigator
	keys responseKeys

	// id is the run on display, 0 before there is one.
	id  RunID
	tab responseTab
	vp  viewport.Model

	// shown is what the viewport holds, so that it is filled only when that changes
	// rather than on every frame.
	shown viewKey

	dialog  dialog
	notice  string
	failure *failure

	// cancelled is set once c was pressed on the run on display. What the cancel
	// did is only known when the run ends.
	cancelled bool
}

// viewKey is what decides the viewport's contents.
type viewKey struct {
	id      RunID
	tab     responseTab
	state   RunState
	dropped bool
	width   int
}

func newResponseScreen(s *session, st styles, cat Catalog, nav navigator) *responseScreen {
	vp := viewport.New()
	vp.KeyMap.Left = key.NewBinding(key.WithKeys("h"))
	vp.KeyMap.Right = key.NewBinding(key.WithKeys("l"))
	return &responseScreen{s: s, st: st, cat: cat, nav: nav, keys: newResponseKeys(), vp: vp}
}

// show puts run id on display.
func (p *responseScreen) show(id RunID) {
	p.id = id
	p.tab = tabJSON
	p.dialog, p.notice, p.failure = nil, "", nil
	p.cancelled = false
	p.shown = viewKey{}
	p.vp.GotoTop()
}

func (p *responseScreen) entry() *runEntry {
	if p.id == 0 {
		return nil
	}
	return p.s.runs.byID[p.id]
}

func (p *responseScreen) request() *sentRequest { return p.s.requests[p.id] }

func (p *responseScreen) focused() bool { return p.dialog != nil }

// tabs are the views this response has. A table is there only for a curated
// command that prints one.
func (p *responseScreen) tabs() []responseTab {
	if req := p.request(); req != nil && req.op.Command.Table && p.cat != nil {
		return []responseTab{tabJSON, tabTable, tabStderr}
	}
	return []responseTab{tabJSON, tabStderr}
}

func (p *responseScreen) moveTab(by int) {
	tabs := p.tabs()
	i := 0
	for j, t := range tabs {
		if t == p.tab {
			i = j
		}
	}
	p.tab = tabs[(i+by+len(tabs))%len(tabs)]
	p.vp.GotoTop()
}

func (p *responseScreen) update(msg tea.Msg) tea.Cmd {
	if d := p.dialog; d != nil {
		finished, cmd := d.update(msg)
		if finished && p.dialog == d {
			p.dialog = nil
		}
		return cmd
	}
	keyMsg, isKey := msg.(tea.KeyPressMsg)
	e := p.entry()
	if !isKey || e == nil {
		return nil
	}

	switch {
	case key.Matches(keyMsg, p.keys.nextTab):
		p.moveTab(1)
	case key.Matches(keyMsg, p.keys.prevTab):
		p.moveTab(-1)
	case key.Matches(keyMsg, p.keys.cancel):
		if e.state != RunDone {
			p.s.runner.Cancel(e.id)
			p.cancelled = true
			p.say("Cancelling…")
		}
	case key.Matches(keyMsg, p.keys.rerun):
		return p.rerun(e)
	case key.Matches(keyMsg, p.keys.save):
		p.askSavePath(e)
	case key.Matches(keyMsg, p.keys.back):
		return p.nav.openRequestScreen()
	default:
		var cmd tea.Cmd
		p.vp, cmd = p.vp.Update(keyMsg)
		return cmd
	}
	return nil
}

func (p *responseScreen) say(notice string) {
	p.notice, p.failure = notice, nil
}

func (p *responseScreen) fail(what string, err error) {
	p.notice = ""
	p.failure = &failure{what: what, result: Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())}}
}

// rerun sends the request again, to the project it went to the first time, and
// asks again first if it is a write.
func (p *responseScreen) rerun(e *runEntry) tea.Cmd {
	req := p.request()
	switch {
	case req == nil:
		p.say("Only a request sent from the Request screen can be sent again from here.")
		return nil
	case e.state != RunDone:
		p.say("It is still running.")
		return nil
	}

	again := &sentRequest{op: req.op, inv: req.inv, project: req.project}
	again.inv.Stdin = bytes.Clone(req.inv.Stdin)
	// The project the run acted on, when fft's own resolution chose it: the active
	// project may have moved since.
	if e.result.Project != "" {
		again.project = e.result.Project
	}
	again.inv.Project = again.project
	a := action{inv: again.inv, display: p.s.displayFor(again.inv.Args, again.project)}

	send := func() tea.Cmd { return p.s.sendRequest(p.nav, a, again) }
	if !req.op.Mutates {
		return send()
	}
	project := again.project
	if project == "" {
		project = "the active project"
	}
	p.dialog = &confirmDialog{
		question: fmt.Sprintf("Send %s to %s again?", firstNonEmpty(req.op.Summary, req.op.ID), project),
		detail:   fmt.Sprintf("%s %s changes data on the tenant.", req.op.Method, req.op.Path),
		command:  a.display,
		onYes:    send,
	}
	return nil
}

// askSavePath asks where to save the response body.
func (p *responseScreen) askSavePath(e *runEntry) {
	switch {
	case e.state != RunDone:
		p.say("It is still running.")
		return
	case e.dropped:
		p.say("This response is no longer held in memory: send the request again to save it.")
		return
	case len(e.result.Stdout) == 0:
		p.say("There is no response body to save.")
		return
	case e.result.StdoutTruncated:
		// A cut-off document saved to a file is one that looks complete to whoever
		// opens it next.
		p.say("The response was too large to keep whole, so it cannot be saved from here. " +
			"Run the command in a shell and redirect its output instead.")
		return
	}

	ext := ".json"
	if !json.Valid(e.result.Stdout) {
		ext = ".out"
	}
	body := e.result.Stdout
	p.dialog = newInputDialog(p.st, "Save the response body to:",
		"Written with mode 0600. A relative path is relative to where fft was started.",
		fmt.Sprintf("response-%d%s", e.id, ext),
		func(path string) tea.Cmd { return p.checkSave(path, body) })
}

// checkSave writes body to path, asking first if that replaces a file.
func (p *responseScreen) checkSave(path string, body []byte) tea.Cmd {
	target, exists, err := savePath(path)
	if err != nil {
		p.fail("saving the response", err)
		return nil
	}
	if !exists {
		p.save(target, body)
		return nil
	}
	p.dialog = &confirmDialog{
		question: "Replace " + target + "?",
		detail:   "The file exists. Its contents will be lost.",
		onYes: func() tea.Cmd {
			p.save(target, body)
			return nil
		},
	}
	return nil
}

func (p *responseScreen) save(target string, body []byte) {
	// atomicfile renames a new 0600 file over the target: a symlink put there is
	// replaced, never followed, and a failed write leaves the old file whole.
	if err := atomicfile.Write(target, body); err != nil {
		p.fail("saving the response", err)
		return
	}
	p.say(fmt.Sprintf("Saved %d bytes to %s.", len(body), target))
}

// savePath resolves where a response is saved, and whether a file is already
// there. It refuses anything but a plain file in an existing directory: saving
// never creates directories, and never writes through a link, into a device or
// over a directory.
func savePath(path string) (target string, exists bool, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false, errors.New("no file named")
	}
	if rest, ok := strings.CutPrefix(path, "~"+string(filepath.Separator)); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, fmt.Errorf("find your home directory: %w", err)
		}
		path = filepath.Join(home, rest)
	}
	target, err = filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("resolve %s: %w", path, err)
	}

	dir, err := os.Stat(filepath.Dir(target))
	switch {
	case err != nil:
		return "", false, fmt.Errorf("%s: the directory must already exist: %w", target, err)
	case !dir.IsDir():
		return "", false, fmt.Errorf("%s is not a directory", filepath.Dir(target))
	}

	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return target, false, nil
	case err != nil:
		return "", false, fmt.Errorf("check %s: %w", target, err)
	case !info.Mode().IsRegular():
		return "", false, fmt.Errorf("%s is not a plain file (%s): choose another name", target, info.Mode().Type())
	default:
		return target, true, nil
	}
}

func (p *responseScreen) bindings() []key.Binding {
	k := p.keys
	e := p.entry()
	switch {
	case p.dialog != nil:
		return p.dialog.bindings()
	case e == nil:
		return nil
	case e.state != RunDone:
		return []key.Binding{k.cancel, k.back}
	}
	keys := []key.Binding{k.nextTab, k.scroll}
	if p.request() != nil {
		keys = append(keys, k.rerun)
	}
	return append(keys, k.save, k.back)
}

func (p *responseScreen) equivalent() shellCommand {
	switch e := p.entry(); {
	case p.dialog != nil:
		return p.dialog.equivalent()
	case e == nil:
		return shellCommand{}
	default:
		return e.display
	}
}

func (p *responseScreen) view(width, height int) string {
	st := p.st
	e := p.entry()
	switch {
	case e == nil && p.id != 0:
		return st.title.Render("Response") + "\n\nThat run is no longer kept: the session remembers its newest runs only."
	case e == nil:
		return st.title.Render("Response") + "\n\nNothing has been sent yet. Send a request from the Request screen (3)."
	}
	// The run's request may have been let go since the tab was chosen.
	if !slices.Contains(p.tabs(), p.tab) {
		p.tab = tabJSON
	}

	lines := []string{p.header(e), st.dim.Render(clip("$ "+output.SanitizeCell(e.display.String()), width))}
	if p.dialog != nil {
		return strings.Join(append(lines, "", p.dialog.view(st, width)), "\n")
	}
	if e.state != RunDone {
		lines = append(lines, "", "Press c to cancel it.")
		if p.notice != "" {
			lines = append(lines, wrap(st.okText.Render(output.SanitizeCell(p.notice)), width))
		}
		return strings.Join(lines, "\n")
	}

	lines = append(lines, p.tabBar())
	// Resolved on the first frame after the run ended, which is the first moment
	// the notice could be read anyway.
	if p.cancelled {
		p.cancelled = false
		p.say(cancelOutcome(e.result))
	}
	for _, note := range p.caveats(e) {
		lines = append(lines, wrap(st.warnText.Render(note), width))
	}
	if p.notice != "" {
		lines = append(lines, wrap(st.okText.Render(output.SanitizeCell(p.notice)), width))
	}
	if p.failure != nil {
		lines = append(lines, p.failure.view(st, width))
	}

	p.fill(e, width, max(height-len(lines)-1, 1))
	return strings.Join(append(lines, "", p.vp.View()), "\n")
}

// cancelOutcome says what a cancel turned out to do: a command that finished its
// work before the cancel reached it reports how it ended, and a write among those
// has landed.
func cancelOutcome(r Result) string {
	if r.ExitCode == exitcode.Interrupted {
		return "Cancelled. A write that had already reached the tenant may still have landed."
	}
	return "It finished before the cancel reached it: this is how it ended."
}

func (p *responseScreen) header(e *runEntry) string {
	st := p.st
	now := p.s.now()
	switch e.state {
	case RunQueued:
		return st.dim.Render("queued " + elapsed(now.Sub(e.queued)))
	case RunRunning:
		return "running " + elapsed(now.Sub(e.started))
	}

	r := e.result
	outcome := fmt.Sprintf("exit %d (%s)", r.ExitCode, exitcode.Meaning(r.ExitCode))
	if r.ExitCode == exitcode.OK {
		outcome = st.okText.Render(outcome)
	} else {
		outcome = st.errorText.Render(outcome)
	}
	parts := []string{outcome}
	if r.Status != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", r.Status))
	} else {
		parts = append(parts, "no HTTP response")
	}
	parts = append(parts, elapsed(r.Duration))
	if r.Project != "" {
		parts = append(parts, output.SanitizeCell(r.Project))
	}
	return strings.Join(parts, " · ")
}

func (p *responseScreen) tabBar() string {
	var parts []string
	for _, t := range p.tabs() {
		if t == p.tab {
			parts = append(parts, p.st.activeTab.Render("["+t.String()+"]"))
		} else {
			parts = append(parts, p.st.tab.Render(" "+t.String()+" "))
		}
	}
	return strings.Join(parts, "")
}

// caveats are what the user must know before trusting what the tabs show.
func (p *responseScreen) caveats(e *runEntry) []string {
	var notes []string
	switch {
	case e.dropped:
		notes = append(notes, fmt.Sprintf("This response is no longer held in memory: the session keeps the newest %d MiB of output.",
			keptOutputBytes>>20))
	case e.result.StdoutTruncated:
		notes = append(notes, fmt.Sprintf("Only the first %s of the response is shown: the rest was too large to keep.",
			byteSize(len(e.result.Stdout))))
	}
	if e.result.StderrTruncated {
		notes = append(notes, "Stderr was too long to keep whole; only its beginning is shown.")
	}
	return notes
}

func byteSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// fill puts the current tab's contents in the viewport, when they have changed.
func (p *responseScreen) fill(e *runEntry, width, height int) {
	p.vp.SetWidth(width)
	p.vp.SetHeight(height)

	k := viewKey{id: e.id, tab: p.tab, state: e.state, dropped: e.dropped, width: width}
	if k == p.shown {
		return
	}
	p.shown = k

	// A table is as wide as its widest row, and wrapping it would break its
	// columns; everything else wraps.
	p.vp.SoftWrap = p.tab != tabTable
	p.vp.SetContent(p.contents(e))
}

func (p *responseScreen) contents(e *runEntry) string {
	r := e.result
	switch p.tab {
	case tabStderr:
		if len(r.Stderr) == 0 {
			return p.st.dim.Render("The command said nothing on stderr.")
		}
		return output.Sanitize(string(r.Stderr))
	case tabTable:
		table, err := p.cat.Table(p.request().op.Command, r.Stdout)
		switch {
		case err != nil:
			return p.st.errorText.Render("The table cannot be drawn: " + output.SanitizeCell(err.Error()))
		case table == "":
			return p.st.dim.Render("No rows.")
		default:
			return output.Sanitize(table)
		}
	}
	return p.document(r)
}

// document is stdout as the JSON tab shows it: indented when it is JSON, as text
// when it is text, and described when it is neither.
func (p *responseScreen) document(r Result) string {
	out := r.Stdout
	switch {
	case len(bytes.TrimSpace(out)) == 0:
		note := "The command printed no data."
		if len(r.Stderr) > 0 {
			note += " See the Stderr view for what it said."
		}
		return p.st.dim.Render(note)
	case json.Valid(out):
		var indented bytes.Buffer
		if err := json.Indent(&indented, out, "", "  "); err == nil {
			return output.Sanitize(indented.String())
		}
	case !utf8.Valid(out) || bytes.IndexByte(out, 0) >= 0:
		return p.st.dim.Render(fmt.Sprintf(
			"The response is %s of binary data, such as a PDF. Press s to save it to a file.", byteSize(len(out))))
	}
	return output.Sanitize(string(out))
}

// inputDialog asks for one line of text.
type inputDialog struct {
	question string
	detail   string
	input    textinput.Model
	onSubmit func(value string) tea.Cmd
}

func newInputDialog(st styles, question, detail, value string, onSubmit func(string) tea.Cmd) *inputDialog {
	in := textinput.New()
	in.Prompt = "> "
	in.SetStyles(st.input)
	in.SetWidth(formInputWidth)
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	return &inputDialog{question: question, detail: detail, input: in, onSubmit: onSubmit}
}

func (d *inputDialog) update(msg tea.Msg) (bool, tea.Cmd) {
	if keyMsg, isKey := msg.(tea.KeyPressMsg); isKey {
		switch {
		case key.Matches(keyMsg, cancelKey):
			return true, nil
		case key.Matches(keyMsg, submitKey):
			return true, d.onSubmit(d.input.Value())
		}
	}
	if paste, isPaste := msg.(tea.PasteMsg); isPaste {
		// Typed, not submitted: enter is still the user's to press.
		paste.Content = strings.TrimRight(paste.Content, "\r\n")
		msg = paste
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return false, cmd
}

func (d *inputDialog) view(st styles, width int) string {
	lines := []string{st.title.Render(output.SanitizeCell(d.question))}
	if d.detail != "" {
		lines = append(lines, output.SanitizeCell(d.detail))
	}
	lines = append(lines, d.input.View(), "", "enter confirm · esc cancel")
	return st.dialog.Width(dialogWidth(width)).Render(strings.Join(lines, "\n"))
}

func (d *inputDialog) bindings() []key.Binding { return []key.Binding{submitKey, cancelKey} }

func (d *inputDialog) equivalent() shellCommand { return shellCommand{} }
