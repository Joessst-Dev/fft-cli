package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// requestField is one row of the request form: a positional argument or a flag.
type requestField struct {
	arg   *Arg
	flag  *Flag
	input textinput.Model
	on    bool
}

func (f *requestField) label() string {
	if f.arg != nil {
		return "<" + f.arg.Name + ">"
	}
	return "--" + f.flag.Name
}

func (f *requestField) required() bool {
	if f.arg != nil {
		return f.arg.Required
	}
	return f.flag.Required
}

func (f *requestField) toggle() bool { return f.flag != nil && f.flag.Kind == FlagBool }

func (f *requestField) value() string { return strings.TrimSpace(f.input.Value()) }

type requestKeys struct {
	up     key.Binding
	down   key.Binding
	edit   key.Binding
	toggle key.Binding
	editor key.Binding
	send   key.Binding
	clear  key.Binding
	back   key.Binding

	done     key.Binding
	next     key.Binding
	prev     key.Binding
	revert   key.Binding
	sendEdit key.Binding
}

func newRequestKeys() requestKeys {
	return requestKeys{
		up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "select")),
		down:   key.NewBinding(key.WithKeys("down", "j")),
		edit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
		toggle: key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "on/off")),
		editor: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit body")),
		send:   key.NewBinding(key.WithKeys("s", "ctrl+s"), key.WithHelp("s", "send")),
		clear:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "clear")),
		back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "operations")),

		done:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
		next:     key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next")),
		prev:     key.NewBinding(key.WithKeys("shift+tab", "up")),
		revert:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "undo")),
		sendEdit: key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "send")),
	}
}

// requestScreen is a form for one operation, built from its command's arguments
// and flags. What it sends is that command line, with the body on stdin.
type requestScreen struct {
	s    *session
	st   styles
	nav  navigator
	keys requestKeys

	op     *Operation
	fields []*requestField
	cursor int

	// editing is set while the selected field has the keyboard; before is what it
	// held when editing started, for esc to put back.
	editing bool
	before  string

	// body is the request body, nil when there is none to send. example is a
	// sample the command printed, kept so that e does not ask for it twice.
	body    []byte
	example []byte

	// busy is set while the example is being fetched or the editor is open, so
	// that e does not open a second one.
	busy bool

	// gen counts the forms opened. An answer that arrives for an earlier one is
	// about a form that is gone.
	gen uint64

	dialog   dialog
	notice   string
	problems []string
	failure  *failure
}

func newRequestScreen(s *session, st styles, nav navigator) *requestScreen {
	return &requestScreen{s: s, st: st, nav: nav, keys: newRequestKeys()}
}

// open replaces the form with one for op.
func (r *requestScreen) open(op Operation) {
	r.gen++
	r.op = &op
	r.fields = nil
	r.cursor, r.editing = 0, false
	r.body, r.example, r.busy = nil, nil, false
	r.dialog, r.notice, r.problems, r.failure = nil, "", nil, nil

	for i := range op.Command.Args {
		r.fields = append(r.fields, r.newField(&op.Command.Args[i], nil))
	}
	for i := range op.Command.Flags {
		r.fields = append(r.fields, r.newField(nil, &op.Command.Flags[i]))
	}
}

func (r *requestScreen) newField(arg *Arg, flag *Flag) *requestField {
	in := textinput.New()
	in.Prompt = ""
	in.SetWidth(formInputWidth)
	in.SetStyles(r.st.input)
	if flag != nil {
		in.Placeholder = placeholderFor(*flag)
	} else {
		in.Placeholder = arg.Name
	}
	f := &requestField{arg: arg, flag: flag, input: in}
	if flag != nil && flag.Kind == FlagBool {
		f.on = flag.Default == "true"
	}
	return f
}

// placeholderFor is the hint an empty field shows: what the command does without
// it, or what kind of value it takes.
func placeholderFor(f Flag) string {
	hint := kindName(f.Kind)
	switch {
	case len(f.Enum) > 0:
		hint = strings.Join(f.Enum, " | ")
	case f.Kind == FlagList:
		hint = "comma-separated"
	}
	if f.Default != "" {
		hint += ", default " + f.Default
	}
	return output.SanitizeCell(hint)
}

func (r *requestScreen) focused() bool { return r.editing || r.dialog != nil }

func (r *requestScreen) selected() *requestField {
	if len(r.fields) == 0 {
		return nil
	}
	r.cursor = min(max(r.cursor, 0), len(r.fields)-1)
	return r.fields[r.cursor]
}

func (r *requestScreen) update(msg tea.Msg) tea.Cmd {
	if d := r.dialog; d != nil {
		finished, cmd := d.update(msg)
		if finished && r.dialog == d {
			r.dialog = nil
		}
		return cmd
	}
	if r.op == nil {
		return nil
	}
	if r.editing {
		return r.editKey(msg)
	}
	keyMsg, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return nil
	}

	switch {
	case key.Matches(keyMsg, r.keys.up):
		r.cursor--
		r.selected()
	case key.Matches(keyMsg, r.keys.down):
		r.cursor++
		r.selected()
	case key.Matches(keyMsg, r.keys.toggle):
		if f := r.selected(); f != nil && f.toggle() {
			f.on = !f.on
		}
	case key.Matches(keyMsg, r.keys.edit):
		f := r.selected()
		switch {
		case f == nil:
		case f.toggle():
			f.on = !f.on
		default:
			r.editing, r.before = true, f.input.Value()
			return f.input.Focus()
		}
	case key.Matches(keyMsg, r.keys.clear):
		if f := r.selected(); f != nil {
			f.input.SetValue("")
			if f.toggle() {
				f.on = f.flag.Default == "true"
			}
		}
	case key.Matches(keyMsg, r.keys.editor):
		return r.editBody()
	case key.Matches(keyMsg, r.keys.send):
		return r.send()
	case key.Matches(keyMsg, r.keys.back):
		return r.nav.openOperations()
	}
	return nil
}

// editKey handles a key or a paste while a field has the keyboard.
func (r *requestScreen) editKey(msg tea.Msg) tea.Cmd {
	f := r.selected()
	if keyMsg, isKey := msg.(tea.KeyPressMsg); isKey {
		switch {
		case key.Matches(keyMsg, r.keys.done):
			r.stopEditing()
			return nil
		case key.Matches(keyMsg, r.keys.revert):
			f.input.SetValue(r.before)
			r.stopEditing()
			return nil
		case key.Matches(keyMsg, r.keys.sendEdit):
			r.stopEditing()
			return r.send()
		case key.Matches(keyMsg, r.keys.next), key.Matches(keyMsg, r.keys.prev):
			r.stopEditing()
			if key.Matches(keyMsg, r.keys.next) {
				r.cursor++
			} else {
				r.cursor--
			}
			// An on/off row takes no typing, so the form stops there.
			if next := r.selected(); next != nil && !next.toggle() {
				r.editing, r.before = true, next.input.Value()
				return next.input.Focus()
			}
			return nil
		}
	}
	if paste, isPaste := msg.(tea.PasteMsg); isPaste {
		// A copied value brings its line break along, which no argument wants.
		paste.Content = strings.TrimRight(paste.Content, "\r\n")
		msg = paste
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return cmd
}

func (r *requestScreen) stopEditing() {
	r.editing = false
	if f := r.selected(); f != nil {
		f.input.Blur()
	}
}

// editBody opens the body in the user's editor, starting from the body so far,
// else from the command's own example, else from the operation's sample.
func (r *requestScreen) editBody() tea.Cmd {
	cmd := r.op.Command
	switch {
	case !cmd.Body:
		r.say(fmt.Sprintf("fft %s takes no request body.", strings.Join(cmd.Path, " ")))
		return nil
	case r.busy:
		return nil
	case r.body != nil:
		return r.launchEditor(r.body)
	case r.example != nil:
		return r.launchEditor(r.example)
	case !cmd.Example:
		return r.launchEditor([]byte(sampleOf(*r.op)))
	}

	// The command prints an example of its own, and the form's flags may shape it:
	// fft connection create --type CUSTOMER prints a customer connection.
	args := r.exampleArgs()
	a := action{inv: Invocation{Args: args}, display: commandLine(args)}
	gen := r.gen
	r.busy = true
	r.say("Fetching the example body…")
	return r.s.start(a, func(res Result) tea.Cmd {
		if gen != r.gen {
			return nil
		}
		r.busy = false
		example := res.Stdout
		note := ""
		if res.ExitCode != exitcode.OK || !json.Valid(example) {
			example = []byte(sampleOf(*r.op))
			note = "The command did not print an example, so this is the operation's sample body."
		}
		r.example = example
		// Only onto the screen that asked, while nothing else has the keyboard: an
		// editor that opened over another screen would take a keystroke meant for it.
		if !r.nav.showing(r) || r.focused() {
			r.say("The example body is ready: press e to edit it.")
			return nil
		}
		cmd := r.launchEditor(example)
		if note != "" && r.failure == nil {
			r.say(note)
		}
		return cmd
	})
}

// exampleArgs is the command line that prints the command's own example: its
// arguments, and those of its flags that may go with --example.
func (r *requestScreen) exampleArgs() []string {
	args := slices.Clone(r.op.Command.Path)
	for _, f := range r.fields {
		if f.flag != nil && !f.flag.WithExample {
			continue
		}
		args = append(args, r.fieldArgs(f)...)
	}
	return append(args, "--example")
}

func sampleOf(op Operation) string {
	if op.SampleBody != "" {
		return op.SampleBody
	}
	return "{}\n"
}

func (r *requestScreen) launchEditor(body []byte) tea.Cmd {
	cmd, err := r.s.openEditor(body, r.gen)
	if err != nil {
		r.fail("opening the editor", err)
		return nil
	}
	r.busy = true
	return cmd
}

// receive takes the editor's exit.
func (r *requestScreen) receive(msg tea.Msg) tea.Cmd {
	done, ok := msg.(editorDoneMsg)
	if !ok {
		return nil
	}
	// The file goes whatever becomes of its contents.
	body, err := r.s.finishEditing(done)
	if done.gen != r.gen {
		return nil
	}
	r.busy = false
	if err != nil {
		r.fail("editing the body", err)
		return nil
	}

	r.problems = nil
	switch {
	case len(bytes.TrimSpace(body)) == 0:
		r.body = nil
		r.say("The body is empty, so none will be sent.")
	case !json.Valid(body):
		// Kept, so that the next e reopens it rather than losing the edit.
		r.body = body
		r.problems = []string{"the body is not valid JSON: press e to fix it"}
	default:
		r.body = body
		r.say("The body is ready to send.")
	}
	return nil
}

func (r *requestScreen) say(notice string) {
	r.notice, r.failure = notice, nil
}

func (r *requestScreen) fail(what string, err error) {
	r.notice = ""
	r.failure = &failure{what: what, result: Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())}}
}

// validate finds what the command would refuse, so that the form can say so before
// anything runs. It is a courtesy: the command checks again.
func (r *requestScreen) validate() []string {
	var problems []string
	for _, f := range r.fields {
		v := f.value()
		if f.toggle() {
			continue
		}
		if v == "" {
			if f.required() {
				problems = append(problems, f.label()+" is required")
			}
			continue
		}
		if f.flag == nil {
			continue
		}
		var err error
		switch f.flag.Kind {
		case FlagInt:
			_, err = strconv.ParseInt(v, 10, 64)
		case FlagFloat:
			_, err = strconv.ParseFloat(v, 64)
		case FlagDuration:
			_, err = time.ParseDuration(v)
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s wants %s, not %q", f.label(), kindName(f.flag.Kind), v))
		}
	}
	cmd := r.op.Command
	switch {
	case cmd.BodyRequired && r.body == nil:
		problems = append(problems, "the body is required: press e to write it")
	case r.body != nil && !json.Valid(r.body):
		problems = append(problems, "the body is not valid JSON: press e to fix it")
	}
	return problems
}

// fieldArgs is what f adds to the command line.
func (r *requestScreen) fieldArgs(f *requestField) []string {
	if f.arg != nil {
		return nil
	}
	name := "--" + f.flag.Name
	switch {
	case f.toggle():
		isDefault := f.on == (f.flag.Default == "true")
		switch {
		case isDefault:
			return nil
		case f.on:
			return []string{name}
		default:
			return []string{name + "=false"}
		}
	case f.value() == "":
		return nil
	case f.flag.Kind == FlagList:
		var out []string
		for v := range strings.SplitSeq(f.value(), ",") {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, flagArg(name, v)...)
			}
		}
		return out
	default:
		return flagArg(name, f.value())
	}
}

// flagArg spells a flag and its value. A value that starts with a dash is joined
// to its flag, so that nothing can read it as a flag of its own.
func flagArg(name, value string) []string {
	if strings.HasPrefix(value, "-") {
		return []string{name + "=" + value}
	}
	return []string{name, value}
}

// args is the command line the form describes. confirmed adds the --yes that
// answers the question the command would ask, for a write the user has confirmed.
func (r *requestScreen) args(confirmed bool) []string {
	cmd := r.op.Command
	args := slices.Clone(cmd.Path)

	var positional []string
	dashed := false
	for _, f := range r.fields {
		if f.arg != nil && f.value() != "" {
			positional = append(positional, f.value())
			dashed = dashed || strings.HasPrefix(f.value(), "-")
		}
	}
	if !dashed {
		args = append(args, positional...)
	}
	for _, f := range r.fields {
		args = append(args, r.fieldArgs(f)...)
	}
	if r.body != nil {
		args = append(args, "--file", "-")
	}
	if confirmed && cmd.Confirms {
		args = append(args, "--yes")
	}
	if dashed {
		// After every flag: an argument that starts with a dash is only an argument
		// once the flags have ended.
		args = append(append(args, "--"), positional...)
	}
	return args
}

// send sends the request, asking first when it is a write.
func (r *requestScreen) send() tea.Cmd {
	if r.busy {
		r.say("The editor has the body: finish there first.")
		return nil
	}
	r.problems = r.validate()
	if len(r.problems) > 0 {
		return nil
	}
	if !r.op.Mutates {
		return r.start(false)
	}

	project := r.s.currentProject()
	if project == "" {
		project = "the active project"
	}
	detail := fmt.Sprintf("%s %s changes data on the tenant.", r.op.Method, r.op.Path)
	if r.s.readOnly() {
		detail += " This project or session is read-only, so fft will refuse it and send nothing."
	}
	r.dialog = &confirmDialog{
		question: fmt.Sprintf("Send %s to %s?", firstNonEmpty(r.op.Summary, r.op.ID), project),
		detail:   detail,
		command:  r.display(true),
		onYes:    func() tea.Cmd { return r.start(true) },
	}
	return nil
}

func (r *requestScreen) display(confirmed bool) shellCommand {
	return r.s.displayFor(r.args(confirmed), r.s.project)
}

func (r *requestScreen) start(confirmed bool) tea.Cmd {
	sent := &sentRequest{
		op: *r.op,
		inv: Invocation{
			Args:  r.args(confirmed),
			Stdin: bytes.Clone(r.body),
		},
		project: r.s.project,
	}
	a := action{inv: sent.inv, display: r.display(confirmed)}
	r.notice, r.failure = "", nil
	return r.s.sendRequest(r.nav, a, sent)
}

// sendRequest starts a request and shows its response, whatever becomes of it.
func (s *session) sendRequest(nav navigator, a action, sent *sentRequest) tea.Cmd {
	id, cmd := s.launch(a, func(Result) tea.Cmd { return nil })
	if id == 0 {
		return cmd
	}
	s.requests[id] = sent
	nav.openResponse(id)
	return cmd
}

func (r *requestScreen) bindings() []key.Binding {
	k := r.keys
	switch {
	case r.dialog != nil:
		return r.dialog.bindings()
	case r.editing:
		return []key.Binding{k.done, k.next, k.revert, k.sendEdit}
	case r.op == nil:
		return nil
	}
	keys := []key.Binding{k.up, k.edit}
	if f := r.selected(); f != nil && f.toggle() {
		keys = append(keys, k.toggle)
	}
	keys = append(keys, k.clear)
	if r.op.Command.Body {
		keys = append(keys, k.editor)
	}
	return append(keys, k.send, k.back)
}

func (r *requestScreen) equivalent() shellCommand {
	switch {
	case r.dialog != nil:
		return r.dialog.equivalent()
	case r.op == nil:
		return shellCommand{}
	}
	return r.display(false)
}

func (r *requestScreen) view(width, _ int) string {
	st := r.st
	if r.op == nil {
		return st.title.Render("Request") + "\n\nChoose an operation on the Operations screen (2), and press enter."
	}
	op := *r.op
	header := []string{
		st.title.Render(output.SanitizeCell(firstNonEmpty(op.Summary, op.ID))),
		st.dim.Render(output.SanitizeCell(op.Method + " " + op.Path + " · " + op.ID)),
	}
	if r.dialog != nil {
		return strings.Join(append(header, "", r.dialog.view(st, width)), "\n")
	}

	lines := header
	if op.Mutates {
		access := "This request changes data: sending it asks first."
		if r.s.readOnly() {
			access = lockBadge + " This project or session is read-only: fft will refuse this write."
		}
		lines = append(lines, st.warnText.Render(access))
	}
	lines = append(lines, "")

	labelWidth := 0
	for _, f := range r.fields {
		labelWidth = max(labelWidth, len(f.label())+len(requiredMark(true)))
	}
	if len(r.fields) == 0 {
		lines = append(lines, st.dim.Render("This command takes no arguments or flags."))
	}
	for i, f := range r.fields {
		lines = append(lines, r.row(f, i == r.cursor, labelWidth, width))
	}
	if f := r.selected(); f != nil && f.flag != nil && f.flag.Usage != "" {
		lines = append(lines, "", st.dim.Render(clip(output.SanitizeCell(f.flag.Usage), width)))
	}

	lines = append(lines, "", r.bodyLine())
	if r.notice != "" {
		lines = append(lines, "", wrap(st.okText.Render(output.SanitizeCell(r.notice)), width))
	}
	for _, p := range r.problems {
		lines = append(lines, st.errorText.Render("• "+output.SanitizeCell(p)))
	}
	if r.failure != nil {
		lines = append(lines, r.failure.view(st, width))
	}
	return strings.Join(lines, "\n")
}

func (r *requestScreen) row(f *requestField, selected bool, labelWidth, width int) string {
	st := r.st
	marker := "  "
	if selected {
		marker = "> "
	}
	label := fmt.Sprintf("%-*s", labelWidth, output.SanitizeCell(f.label())+requiredMark(f.required()))
	if selected {
		label = st.selected.Render(label)
	}

	var val string
	switch {
	case f.toggle():
		val = "[ ]"
		if f.on {
			val = "[x]"
		}
	case selected && r.editing:
		val = f.input.View()
	case f.value() == "":
		val = st.dim.Render(f.input.Placeholder)
	default:
		val = output.SanitizeCell(f.input.Value())
	}
	return clip(marker+label+"  "+val, width)
}

func (r *requestScreen) bodyLine() string {
	cmd := r.op.Command
	switch {
	case !cmd.Body:
		return r.st.dim.Render("Body: this command takes none.")
	case r.body != nil:
		return fmt.Sprintf("Body: %d bytes, sent on stdin (e to edit).", len(r.body))
	case cmd.BodyRequired:
		return r.st.warnText.Render("Body: required. Press e to write it, starting from an example.")
	default:
		return r.st.dim.Render("Body: none. Press e to write one, starting from an example.")
	}
}
