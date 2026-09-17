package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// templateRow is one entry of `fft template list -o json`.
type templateRow struct {
	Name        string   `json:"name"`
	Scope       string   `json:"scope"`
	OperationID string   `json:"operationId"`
	Project     string   `json:"project"`
	Params      []string `json:"params"`
	Description string   `json:"description"`
	Path        string   `json:"path"`
}

// projectScope is the scope `fft template list` reports for ./.fft/templates, the
// one `--local` reaches.
const projectScope = "project"

// templateDoc is `fft template show <name> -o json`: the template file itself.
type templateDoc struct {
	Description string                   `json:"description"`
	OperationID string                   `json:"operationId"`
	Project     string                   `json:"project"`
	Params      map[string]templateParam `json:"params"`

	// Body is kept as the bytes fft printed, so that a 64-bit id is shown as the
	// digits it is.
	Body json.RawMessage `json:"body"`
}

// templateParam is one parameter a template declares.
type templateParam struct {
	Path        string          `json:"path"`
	Required    bool            `json:"required"`
	Default     json.RawMessage `json:"default"`
	Description string          `json:"description"`
}

// stringDefault reports whether the parameter's default is a JSON string. A value
// typed for such a parameter is sent as a string whatever it looks like: an id made
// only of digits would otherwise go out as a number.
func (p templateParam) stringDefault() bool {
	return len(p.Default) > 0 && p.Default[0] == '"'
}

// paramField is one row of a template's parameters form.
type paramField struct {
	name  string
	spec  templateParam
	input textinput.Model
}

func (f *paramField) value() string { return strings.TrimSpace(f.input.Value()) }

// setArgs is what f adds to `fft template render`.
func (f *paramField) setArgs() []string {
	if f.value() == "" {
		return nil
	}
	flag := "--set"
	if f.spec.stringDefault() {
		flag = "--set-string"
	}
	return flagArg(flag, f.name+"="+f.value())
}

// openTemplate is the template whose detail is on display.
type openTemplate struct {
	row templateRow

	// doc is what `template show` said, nil while it is being read.
	doc    *templateDoc
	params []*paramField

	// form is set while the parameters have the cursor, editing while one of them
	// has the keyboard; before is what it held when editing started.
	form    bool
	cursor  int
	editing bool
	before  string

	// rendered is the body the last render printed, and warnings what it said
	// about it; nil until R or S has rendered it.
	rendered []byte
	warnings []string

	// scroll is how many lines of the body are scrolled past.
	scroll int
}

func (o *openTemplate) selected() *paramField {
	if len(o.params) == 0 {
		return nil
	}
	o.cursor = min(max(o.cursor, 0), len(o.params)-1)
	return o.params[o.cursor]
}

type templateKeys struct {
	up     key.Binding
	down   key.Binding
	open   key.Binding
	params key.Binding
	render key.Binding
	send   key.Binding
	remove key.Binding
	reload key.Binding
	back   key.Binding
	scroll key.Binding

	edit   key.Binding
	clear  key.Binding
	done   key.Binding
	next   key.Binding
	prev   key.Binding
	revert key.Binding
	leave  key.Binding
}

func newTemplateKeys() templateKeys {
	return templateKeys{
		up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "select")),
		down:   key.NewBinding(key.WithKeys("down", "j")),
		open:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		params: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "parameters")),
		render: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "render")),
		send:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "render and send")),
		remove: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "remove")),
		reload: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reload")),
		back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "templates")),
		scroll: key.NewBinding(key.WithKeys("up", "down", "k", "j"), key.WithHelp("↑/↓", "scroll")),

		edit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
		clear:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "clear")),
		done:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
		next:   key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next")),
		prev:   key.NewBinding(key.WithKeys("shift+tab", "up")),
		revert: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "undo")),
		leave:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "done")),
	}
}

// templatesScreen lists the saved templates, renders them with the values the user
// gives their parameters, and sends what they render through the operation's
// command — each step through the fft command that does it.
type templatesScreen struct {
	s    *session
	st   styles
	nav  navigator
	keys templateKeys

	// ops is every operation the catalog offers, by id: a template names the one it
	// is for, and the catalog knows the command that sends it.
	ops map[string]Operation

	rows   []templateRow
	notes  []string
	cursor int
	loaded bool

	// requested is set once the list has been asked for since it last changed. The
	// list is read when the screen is first shown, not when the UI starts.
	requested bool

	// gen counts the lists read and the templates opened. An answer that arrives
	// for an earlier one is about something no longer on screen.
	gen uint64

	open *openTemplate

	// values are what the user gave each template's parameters, by template name,
	// kept for as long as the session runs.
	values map[string]map[string]string

	dialog  dialog
	notice  string
	failure *failure
}

func newTemplatesScreen(s *session, st styles, nav navigator, cat Catalog) *templatesScreen {
	t := &templatesScreen{
		s: s, st: st, nav: nav, keys: newTemplateKeys(),
		ops:    make(map[string]Operation),
		values: make(map[string]map[string]string),
	}
	if cat != nil {
		for _, g := range cat.Groups() {
			for _, op := range g.Operations {
				t.ops[op.ID] = op
			}
		}
	}
	return t
}

func (t *templatesScreen) focused() bool {
	return t.dialog != nil || (t.open != nil && t.open.editing)
}

// shown is called on every update while the screen is on display, and reads the
// list the first time it is.
func (t *templatesScreen) shown() tea.Cmd {
	if t.requested {
		return nil
	}
	return t.reload()
}

// changed says a template was saved or removed elsewhere, so that the list is read
// again the next time it is shown.
func (t *templatesScreen) changed() {
	t.requested = false
}

func (t *templatesScreen) selected() (templateRow, bool) {
	if len(t.rows) == 0 {
		return templateRow{}, false
	}
	t.cursor = min(max(t.cursor, 0), len(t.rows)-1)
	return t.rows[t.cursor], true
}

func (t *templatesScreen) say(notice string) {
	t.notice, t.failure = notice, nil
}

func (t *templatesScreen) fail(what string, r Result) {
	t.notice = ""
	t.failure = &failure{what: what, result: r}
}

func (t *templatesScreen) reload() tea.Cmd {
	t.requested = true
	t.gen++
	gen := t.gen
	args := []string{"template", "list"}
	return t.s.start(action{inv: Invocation{Args: args}, display: commandLine(args)}, func(r Result) tea.Cmd {
		if gen != t.gen {
			return nil
		}
		if r.ExitCode != exitcode.OK {
			t.fail("listing the templates", r)
			return nil
		}
		var rows []templateRow
		if err := json.Unmarshal(r.Stdout, &rows); err != nil {
			t.fail("reading the template list", Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())})
			return nil
		}
		// The selection follows the template, not the row: a template saved since
		// the last read moves the others down, and x on the row the cursor was left
		// at would otherwise name a template the user never picked.
		previous, had := t.selected()
		t.rows, t.loaded = rows, true
		if had {
			for i, row := range rows {
				if row.Name == previous.Name && row.Scope == previous.Scope {
					t.cursor = i
				}
			}
		}
		// What list said on stderr: files it could not read, templates a project one
		// hides. Each is about the list on screen, and would otherwise go unseen.
		t.notes = stderrTail(r.Stderr, 4)
		t.selected()
		return nil
	})
}

func (t *templatesScreen) update(msg tea.Msg) tea.Cmd {
	if d := t.dialog; d != nil {
		finished, cmd := d.update(msg)
		if finished && t.dialog == d {
			t.dialog = nil
		}
		return cmd
	}
	if o := t.open; o != nil && o.editing {
		return t.editKey(o, msg)
	}
	keyMsg, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return nil
	}
	switch o := t.open; {
	case o == nil:
		return t.listKey(keyMsg)
	case o.form:
		return t.formKey(o, keyMsg)
	default:
		return t.detailKey(o, keyMsg)
	}
}

func (t *templatesScreen) listKey(msg tea.KeyPressMsg) tea.Cmd {
	row, ok := t.selected()
	switch {
	case key.Matches(msg, t.keys.up):
		t.cursor--
		t.selected()
	case key.Matches(msg, t.keys.down):
		t.cursor++
		t.selected()
	case key.Matches(msg, t.keys.reload):
		return t.reload()
	case !ok:
	case key.Matches(msg, t.keys.open):
		return t.openRow(row, nil)
	case key.Matches(msg, t.keys.params):
		return t.openRow(row, t.startForm)
	case key.Matches(msg, t.keys.render):
		return t.openRow(row, t.renderOnly)
	case key.Matches(msg, t.keys.send):
		return t.openRow(row, t.renderAndSend)
	case key.Matches(msg, t.keys.remove):
		return t.remove(row)
	}
	return nil
}

func (t *templatesScreen) detailKey(o *openTemplate, msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, t.keys.back):
		t.close()
	case key.Matches(msg, t.keys.up):
		o.scroll = max(o.scroll-1, 0)
	case key.Matches(msg, t.keys.down):
		o.scroll = min(o.scroll+1, t.maxScroll(o))
	case o.doc == nil:
		// Everything below needs what show says, and it has not said it yet.
	case key.Matches(msg, t.keys.params):
		return t.startForm(o)
	case key.Matches(msg, t.keys.render):
		return t.renderOnly(o)
	case key.Matches(msg, t.keys.send):
		return t.renderAndSend(o)
	case key.Matches(msg, t.keys.remove):
		return t.remove(o.row)
	}
	return nil
}

func (t *templatesScreen) formKey(o *openTemplate, msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, t.keys.leave):
		o.form = false
	case key.Matches(msg, t.keys.up):
		o.cursor--
		o.selected()
	case key.Matches(msg, t.keys.down):
		o.cursor++
		o.selected()
	case key.Matches(msg, t.keys.edit):
		if f := o.selected(); f != nil {
			o.editing, o.before = true, f.input.Value()
			return f.input.Focus()
		}
	case key.Matches(msg, t.keys.clear):
		if f := o.selected(); f != nil {
			f.input.SetValue("")
			t.keep(o)
		}
	case key.Matches(msg, t.keys.render):
		return t.renderOnly(o)
	case key.Matches(msg, t.keys.send):
		return t.renderAndSend(o)
	}
	return nil
}

// editKey handles a key or a paste while a parameter has the keyboard.
func (t *templatesScreen) editKey(o *openTemplate, msg tea.Msg) tea.Cmd {
	f := o.selected()
	if keyMsg, isKey := msg.(tea.KeyPressMsg); isKey {
		switch {
		case key.Matches(keyMsg, t.keys.done):
			t.stopEditing(o)
			return nil
		case key.Matches(keyMsg, t.keys.revert):
			f.input.SetValue(o.before)
			t.stopEditing(o)
			return nil
		case key.Matches(keyMsg, t.keys.next), key.Matches(keyMsg, t.keys.prev):
			t.stopEditing(o)
			if key.Matches(keyMsg, t.keys.next) {
				o.cursor++
			} else {
				o.cursor--
			}
			next := o.selected()
			o.editing, o.before = true, next.input.Value()
			return next.input.Focus()
		}
	}
	if paste, isPaste := msg.(tea.PasteMsg); isPaste {
		// A copied value brings its line break along, which no parameter wants.
		paste.Content = strings.TrimRight(paste.Content, "\r\n")
		msg = paste
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return cmd
}

func (t *templatesScreen) stopEditing(o *openTemplate) {
	o.editing = false
	if f := o.selected(); f != nil {
		f.input.Blur()
	}
	t.keep(o)
}

// keep remembers o's parameter values, so that the template opens with them again.
func (t *templatesScreen) keep(o *openTemplate) {
	kept := make(map[string]string, len(o.params))
	for _, f := range o.params {
		if v := f.value(); v != "" {
			kept[f.name] = v
		}
	}
	t.values[o.row.Name] = kept
}

func (t *templatesScreen) close() {
	t.gen++
	t.open = nil
}

// openRow shows row's detail, reading it with `template show`, and then does next
// with it, if the detail is still on display by the time it has been read.
func (t *templatesScreen) openRow(row templateRow, next func(*openTemplate) tea.Cmd) tea.Cmd {
	t.gen++
	gen := t.gen
	o := &openTemplate{row: row}
	t.open = o
	t.say("")
	args := []string{"template", "show", row.Name}
	return t.s.start(action{inv: Invocation{Args: args}, display: commandLine(args)}, func(r Result) tea.Cmd {
		if gen != t.gen {
			return nil
		}
		if r.ExitCode != exitcode.OK {
			t.fail("reading "+row.Name, r)
			return nil
		}
		var doc templateDoc
		if err := json.Unmarshal(r.Stdout, &doc); err != nil {
			t.fail("reading "+row.Name, Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())})
			return nil
		}
		t.setDoc(o, &doc)
		// Only onto the screen that asked, while nothing else has the keyboard: a
		// form or a dialog that opened over another screen would take its keys.
		if next == nil || !t.nav.showing(t) || t.focused() {
			return nil
		}
		return next(o)
	})
}

func (t *templatesScreen) setDoc(o *openTemplate, doc *templateDoc) {
	o.doc = doc
	names := make([]string, 0, len(doc.Params))
	for name := range doc.Params {
		names = append(names, name)
	}
	slices.Sort(names)

	kept := t.values[o.row.Name]
	o.params = make([]*paramField, 0, len(names))
	for _, name := range names {
		spec := doc.Params[name]
		in := textinput.New()
		in.Prompt = ""
		in.SetWidth(formInputWidth)
		in.SetStyles(t.st.input)
		in.Placeholder = paramPlaceholder(spec)
		in.SetValue(kept[name])
		o.params = append(o.params, &paramField{name: name, spec: spec, input: in})
	}
}

// paramPlaceholder is what an empty parameter shows: whether the render needs it,
// and what it sends without it.
func paramPlaceholder(p templateParam) string {
	switch {
	case p.Required:
		return "required"
	case len(p.Default) > 0:
		return "default " + output.SanitizeCell(string(p.Default))
	default:
		return "unchanged"
	}
}

func (t *templatesScreen) startForm(o *openTemplate) tea.Cmd {
	if len(o.params) == 0 {
		t.say(o.row.Name + " declares no parameters. Render it as it is with R.")
		return nil
	}
	o.form = true
	o.cursor = 0
	return nil
}

// missing is the required parameters o has no value for.
func (o *openTemplate) missing() []string {
	var names []string
	for _, f := range o.params {
		if f.spec.Required && f.value() == "" {
			names = append(names, f.name)
		}
	}
	return names
}

// renderArgs is the `fft template render` command line for o's values.
func (o *openTemplate) renderArgs() []string {
	args := []string{"template", "render", o.row.Name}
	for _, f := range o.params {
		args = append(args, f.setArgs()...)
	}
	return args
}

// ready says whether o can be rendered, and opens its parameters when it cannot.
func (t *templatesScreen) ready(o *openTemplate) bool {
	missing := o.missing()
	if len(missing) == 0 {
		return true
	}
	o.form = true
	for i, f := range o.params {
		if f.name == missing[0] {
			o.cursor = i
		}
	}
	t.say("Give a value for " + strings.Join(missing, ", ") + " first: the template requires it.")
	return false
}

// render runs `template render` for o, for project, and hands done what it printed
// and what it warned about.
func (t *templatesScreen) render(o *openTemplate, project string, done func(body []byte, warnings []string) tea.Cmd) tea.Cmd {
	args := o.renderArgs()
	a := action{inv: Invocation{Args: args, Project: project}, display: t.s.displayFor(args, project)}
	gen := t.gen
	t.say("Rendering " + o.row.Name + "…")
	return t.s.start(a, func(r Result) tea.Cmd {
		if gen != t.gen || t.open != o {
			return nil
		}
		if r.ExitCode != exitcode.OK {
			o.rendered, o.warnings = nil, nil
			t.fail("rendering "+o.row.Name, r)
			return nil
		}
		o.rendered = bytes.Clone(r.Stdout)
		o.warnings = stderrTail(r.Stderr, 8)
		o.scroll = 0
		t.say("")
		return done(o.rendered, o.warnings)
	})
}

func (t *templatesScreen) renderOnly(o *openTemplate) tea.Cmd {
	if !t.ready(o) {
		return nil
	}
	return t.render(o, t.s.target(), func([]byte, []string) tea.Cmd {
		t.say("Rendered. S sends it; nothing has been sent yet.")
		return nil
	})
}

// sender is the operation o's rendered body is sent with, and why there is none.
func (t *templatesScreen) sender(o *openTemplate) (Operation, string) {
	id := o.doc.OperationID
	if id == "" {
		return Operation{}, o.row.Name + " names no operation, so there is no command to send it with. " +
			"Render it with R, and pipe it into the command you want in a shell."
	}
	op, ok := t.ops[id]
	switch {
	case !ok:
		return Operation{}, "This fft has no operation " + id + ": the API may have changed since the template was saved."
	case !op.Command.Body:
		return Operation{}, fmt.Sprintf("fft %s takes no request body, so there is nothing to send %s with.",
			strings.Join(op.Command.Path, " "), o.row.Name)
	}
	return op, ""
}

// renderAndSend renders o and sends the body it prints through its operation's
// command, on stdin.
//
// The project is decided once, here: the render compares the template with it, and
// the send goes to it. Anything the render warns about is put to the user before
// the body goes anywhere, and the send itself is the Request screen's, so that a
// write is asked about — or asks its own question — exactly as it is there.
func (t *templatesScreen) renderAndSend(o *openTemplate) tea.Cmd {
	op, why := t.sender(o)
	if why != "" {
		t.say(why)
		return nil
	}
	if !t.ready(o) {
		return nil
	}
	project := t.s.target()
	asked := t.s.switches
	return t.render(o, project, func(body []byte, warnings []string) tea.Cmd {
		if t.s.switches != asked {
			t.say("The project changed while the template rendered, so nothing was sent. Press S to render it for " +
				t.s.named() + ".")
			return nil
		}
		// Only from the screen that asked, while nothing else has the keyboard: a
		// send that took the user off another screen, or a question that took its
		// keys, would act on a keystroke meant for something else.
		if !t.nav.showing(t) || t.focused() {
			t.say("Rendered " + o.row.Name + ", and nothing was sent: you had moved on. Press S again to send it.")
			return nil
		}
		send := func() tea.Cmd { return t.handOver(op, body, project, asked) }
		if len(warnings) == 0 {
			return send()
		}
		t.dialog = armed(&confirmDialog{
			question: fmt.Sprintf("Rendering %s warned. Send it anyway?", o.row.Name),
			notes:    warnings,
			detail:   "Nothing has been sent yet. A write is asked about again before it goes.",
			command:  t.pipeline(o, op, project),
			onYes:    send,
		}, t.s.now, true)
		return nil
	})
}

// handOver gives the rendered body to the Request screen, which sends it the way it
// sends a body the user wrote.
func (t *templatesScreen) handOver(op Operation, body []byte, project string, asked uint64) tea.Cmd {
	if t.s.switches != asked || t.s.target() != project {
		t.say("The project changed before the template was sent, so nothing was sent. Press S to render it for " +
			t.s.named() + ".")
		return nil
	}
	return t.nav.sendBody(op, body)
}

// remove runs `template remove`, which asks its own question before it deletes
// anything.
func (t *templatesScreen) remove(row templateRow) tea.Cmd {
	args := []string{"template", "remove", row.Name}
	if row.Scope == projectScope {
		args = append(args, "--local")
	}
	t.say("Removing " + row.Name + "…")
	return t.s.start(action{inv: Invocation{Args: args}, display: commandLine(args)}, func(r Result) tea.Cmd {
		if r.ExitCode != exitcode.OK {
			t.fail("removing "+row.Name, r)
			return t.reload()
		}
		t.say("Removed " + row.Name + ".")
		if t.open != nil && t.open.row.Name == row.Name {
			t.close()
		}
		delete(t.values, row.Name)
		return t.reload()
	})
}

// pipeline is the shell pipe that does what S does: the render, into the
// operation's command.
func (t *templatesScreen) pipeline(o *openTemplate, op Operation, project string) shellCommand {
	render := t.s.displayFor(o.renderArgs(), project)
	send := t.s.displayFor(append(slices.Clone(op.Command.Path), "--file", "-"), project)
	return shellCommand{
		line:       render.line + " | " + send.line,
		unportable: render.unportable || send.unportable,
	}
}

func (t *templatesScreen) bindings() []key.Binding {
	k := t.keys
	o := t.open
	switch {
	case t.dialog != nil:
		return t.dialog.bindings()
	case o != nil && o.editing:
		return []key.Binding{k.done, k.next, k.revert}
	case o != nil && o.form:
		return []key.Binding{k.up, k.edit, k.clear, k.render, k.send, k.leave}
	case o != nil:
		keys := []key.Binding{k.scroll}
		if o.doc != nil {
			keys = append(keys, k.params, k.render, k.send, k.remove)
		}
		return append(keys, k.back)
	case len(t.rows) == 0:
		return []key.Binding{k.reload}
	default:
		return []key.Binding{k.up, k.open, k.params, k.render, k.send, k.remove, k.reload}
	}
}

func (t *templatesScreen) equivalent() shellCommand {
	if t.dialog != nil {
		return t.dialog.equivalent()
	}
	o := t.open
	if o == nil {
		if row, ok := t.selected(); ok {
			return commandLine([]string{"template", "show", row.Name})
		}
		return commandLine([]string{"template", "list"})
	}
	if o.doc != nil {
		if op, why := t.sender(o); why == "" {
			return t.pipeline(o, op, t.s.target())
		}
	}
	return t.s.displayFor(o.renderArgs(), t.s.target())
}

func (t *templatesScreen) view(width, height int) string {
	st := t.st
	if t.dialog != nil {
		// A dialog has the keyboard, so it is drawn first, where no height can cut it.
		return strings.Join([]string{st.title.Render("Templates"), "", t.dialog.view(st, width)}, "\n")
	}
	if t.open != nil {
		return t.detail(t.open, width, height)
	}

	lines := []string{st.title.Render("Templates"), ""}
	switch {
	case !t.loaded && t.failure == nil:
		lines = append(lines, st.dim.Render("Loading templates…"))
	case t.loaded && len(t.rows) == 0:
		lines = append(lines, "No templates are saved yet. Save a request as one with t on the Request screen, "+
			"or with 'fft template save' in a shell.")
	default:
		lines = append(lines, t.table(width)...)
	}
	for _, note := range t.notes {
		lines = append(lines, st.warnText.Render(clip(note, width)))
	}
	return strings.Join(append(lines, t.footer(width)...), "\n")
}

// footer is the notice or the failure under whatever the screen shows.
func (t *templatesScreen) footer(width int) []string {
	var lines []string
	if t.notice != "" {
		lines = append(lines, "", wrap(t.st.okText.Render(output.SanitizeCell(t.notice)), width))
	}
	if t.failure != nil {
		lines = append(lines, "", t.failure.view(t.st, width))
	}
	return lines
}

var templateColumns = []string{"NAME", "SCOPE", "OPERATION", "PARAMETERS", "DESCRIPTION"}

func (t *templatesScreen) table(width int) []string {
	cells := make([][]string, 0, len(t.rows)+1)
	cells = append(cells, templateColumns)
	for _, r := range t.rows {
		// Everything but the name comes out of the file, and a project template
		// arrives with a git clone.
		cells = append(cells, []string{
			output.SanitizeCell(r.Name),
			output.SanitizeCell(r.Scope),
			output.SanitizeCell(r.OperationID),
			output.SanitizeCell(strings.Join(r.Params, ", ")),
			output.SanitizeCell(r.Description),
		})
	}

	widths := make([]int, len(templateColumns))
	for _, row := range cells {
		for i, c := range row {
			widths[i] = max(widths[i], ansi.StringWidth(c))
		}
	}

	lines := make([]string, 0, len(cells))
	for i, row := range cells {
		var b strings.Builder
		for j, c := range row {
			b.WriteString(c)
			if j < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-ansi.StringWidth(c)+2))
			}
		}
		line := b.String()
		switch {
		case i == 0:
			line = t.st.dim.Render("  " + line)
		case i-1 == t.cursor:
			line = t.st.selected.Render("> " + line)
		default:
			line = "  " + line
		}
		lines = append(lines, clip(line, width))
	}
	return lines
}

// detail is o as the screen shows it: what the template is for, what it sends, its
// parameters, and its body — rendered, once it has been.
func (t *templatesScreen) detail(o *openTemplate, width, height int) string {
	st := t.st
	clean := output.SanitizeCell
	head := []string{
		st.title.Render(clean(o.row.Name)) + "  " + st.dim.Render(clean(o.row.Scope)+" scope"),
		st.dim.Render(clip(clean(o.row.Path), width)),
	}
	doc := o.doc
	if doc == nil {
		head = append(head, "", st.dim.Render("Reading the template…"))
		return strings.Join(append(head, t.footer(width)...), "\n")
	}

	if doc.Description != "" {
		head = append(head, wrap(clean(doc.Description), width))
	}
	head = append(head, "", t.operationLine(doc))
	if doc.Project != "" {
		saved := "saved under " + clean(doc.Project)
		if cur := t.s.currentProject(); cur != "" && cur != doc.Project {
			saved = st.warnText.Render(saved + ", not " + clean(cur) + ": ids in the body may not resolve here")
		}
		head = append(head, saved)
	}

	head = append(head, "", st.title.Render("Parameters"))
	if len(o.params) == 0 {
		head = append(head, st.dim.Render("  none declared"))
	}
	nameWidth := 0
	for _, f := range o.params {
		nameWidth = max(nameWidth, ansi.StringWidth(clean(f.name)))
	}
	for i, f := range o.params {
		head = append(head, t.paramRow(o, f, o.form && i == o.cursor, nameWidth, width))
	}
	if f := o.selected(); o.form && f != nil && f.spec.Description != "" {
		head = append(head, st.dim.Render(clip("  "+clean(f.spec.Description), width)))
	}

	for _, w := range o.warnings {
		head = append(head, wrap(st.warnText.Render(clean(w)), width))
	}
	head = append(head, t.footer(width)...)

	title := "Saved body"
	body := []byte(doc.Body)
	if o.rendered != nil {
		title, body = "Rendered body", o.rendered
	}
	head = append(head, "", st.title.Render(title))

	lines := bodyLines(body)
	room := max(height-len(head), 1)
	first := min(o.scroll, max(len(lines)-room, 0))
	shown := lines[first:min(first+room, len(lines))]
	for i, line := range shown {
		shown[i] = clip(line, width)
	}
	return strings.Join(append(head, shown...), "\n")
}

// maxScroll is the furthest o's body scrolls: far enough to show its last line.
func (t *templatesScreen) maxScroll(o *openTemplate) int {
	body := []byte{}
	if o.doc != nil {
		body = o.doc.Body
	}
	if o.rendered != nil {
		body = o.rendered
	}
	return max(len(bodyLines(body))-1, 0)
}

// bodyLines is a JSON body indented, one line per element, stripped of anything a
// terminal would act on: it is the file's, and a project file comes with a clone.
func bodyLines(body []byte) []string {
	var indented bytes.Buffer
	text := string(body)
	if err := json.Indent(&indented, body, "  ", "  "); err == nil {
		text = "  " + indented.String()
	}
	return strings.Split(strings.TrimRight(output.Sanitize(text), "\n"), "\n")
}

// operationLine says which operation the template is for, and the command that
// sends it from here.
func (t *templatesScreen) operationLine(doc *templateDoc) string {
	id := output.SanitizeCell(doc.OperationID)
	if id == "" {
		return t.st.dim.Render("operation  none recorded: S cannot send it")
	}
	op, ok := t.ops[doc.OperationID]
	if !ok {
		return t.st.warnText.Render("operation  " + id + " (this fft does not know it)")
	}
	line := fmt.Sprintf("operation  %s  %s %s  →  fft %s", id, op.Method, output.SanitizeCell(op.Path),
		output.SanitizeCell(strings.Join(op.Command.Path, " ")))
	if op.Mutates {
		line += "  " + t.st.warnText.Render("(a write)")
	}
	return line
}

func (t *templatesScreen) paramRow(o *openTemplate, f *paramField, selected bool, nameWidth, width int) string {
	st := t.st
	marker := "  "
	if selected {
		marker = "> "
	}
	name := output.SanitizeCell(f.name)
	label := name + strings.Repeat(" ", max(nameWidth-ansi.StringWidth(name), 0))
	if selected {
		label = st.selected.Render(label)
	}
	var val string
	switch {
	case selected && o.editing:
		val = f.input.View()
	case f.value() == "":
		val = st.dim.Render(f.input.Placeholder)
	default:
		val = output.SanitizeCell(f.input.Value())
	}
	path := st.dim.Render(output.SanitizeCell(f.spec.Path))
	return clip(marker+label+"  "+path+"  "+val, width)
}
