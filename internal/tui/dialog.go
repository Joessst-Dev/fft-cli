package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// dialog is a question that has the keyboard until it is answered.
type dialog interface {
	// update handles a key or a paste. It returns whether the dialog is finished,
	// and what answering it started.
	update(msg tea.Msg) (finished bool, cmd tea.Cmd)
	// view draws the dialog width columns wide, and within height rows where it
	// can; 0 is a height nobody knows.
	view(st styles, width, height int) string
	bindings() []key.Binding

	// equivalent is the command a yes would run, empty when a yes runs nothing yet.
	equivalent() shellCommand
}

var (
	yesKey    = key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "yes"))
	noKey     = key.NewBinding(key.WithKeys("n", "N", "esc", "enter"), key.WithHelp("n/esc", "no"))
	submitKey = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm"))
	cancelKey = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
)

// questionLegend is what the keys of a question do. A question has the keyboard
// while it is open, so the legend, which cannot be opened then, explains them
// ahead of time.
func questionLegend() legendSection {
	return legendSection{
		title:   "In a question",
		compact: true,
		entries: []legendEntry{
			{of: yesKey, desc: "yes"},
			{of: noKey, keys: "n/esc/enter", desc: "no"},
			{of: submitKey, desc: "confirm a typed name or value"},
			{of: cancelKey, desc: "cancel"},
		},
	}
}

// armDelay is how long a dialog ignores keys once it is in front of the user. A
// key meant for what was there a moment before — the next letter of a value, a y
// typed just after ctrl+s sent the form — arrives inside it, and must not answer a
// question nobody has read yet.
const armDelay = 500 * time.Millisecond

// armedDialog is a dialog that takes no keys until it has been in front of the
// user for armDelay.
type armedDialog struct {
	dialog
	now func() time.Time

	// shown is when the dialog first had the keyboard, zero until then.
	shown time.Time
}

// armed wraps d. A dialog opened by the key the user just pressed is in front of
// them at once, and shown is set; one that waits its turn is shown later, by arm.
func armed(d dialog, now func() time.Time, shown bool) *armedDialog {
	a := &armedDialog{dialog: d, now: now}
	if shown {
		a.arm()
	}
	return a
}

// arm notes that the dialog now has the keyboard. Only the first call counts.
func (a *armedDialog) arm() {
	if a.shown.IsZero() {
		a.shown = a.now()
	}
}

func (a *armedDialog) update(msg tea.Msg) (bool, tea.Cmd) {
	if a.shown.IsZero() || a.now().Sub(a.shown) < armDelay {
		return false, nil
	}
	return a.dialog.update(msg)
}

// confirmDialog asks a yes/no question. No is the default: enter declines, and
// only y goes ahead.
type confirmDialog struct {
	question string

	// notes are lines the question is about, each drawn on its own: the warnings a
	// command printed, say.
	notes []string

	detail  string
	command shellCommand
	onYes   func() tea.Cmd

	// preview is the request body a yes sends, nil for a question about none.
	preview *bodyPreview
}

func (d *confirmDialog) update(msg tea.Msg) (bool, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyPressMsg)
	switch {
	case !isKey:
		// Pasted text answers nothing: only a key pressed on purpose may say yes.
	case key.Matches(keyMsg, yesKey):
		return true, d.onYes()
	case key.Matches(keyMsg, noKey):
		return true, nil
	}
	return false, nil
}

func (d *confirmDialog) view(st styles, width, height int) string {
	top := []string{st.title.Render(output.SanitizeCell(d.question))}
	for _, note := range d.notes {
		top = append(top, st.warnText.Render(output.SanitizeCell(note)))
	}
	if d.detail != "" {
		top = append(top, output.SanitizeCell(d.detail))
	}
	var bottom []string
	if !d.command.empty() {
		bottom = append(bottom, "", st.dim.Render("runs: "+output.SanitizeCell(d.command.String())))
	}
	bottom = append(bottom, "", "y yes · n no")
	return framed(st, width, height, top, d.preview, bottom)
}

func (d *confirmDialog) bindings() []key.Binding { return []key.Binding{yesKey, noKey} }

func (d *confirmDialog) equivalent() shellCommand { return d.command }

// typeNameDialog asks for a name to be typed back before something that cannot be
// undone, or that takes a protection away. A y is too easy to give by accident —
// it is also the key that copies a command — and the name of the thing is not.
type typeNameDialog struct {
	question string
	detail   string
	name     string

	// what is what name is, as the dialog calls it: "name", unless set.
	what string

	// notes are lines the question is about, each drawn on its own.
	notes []string

	command  shellCommand
	input    textinput.Model
	mismatch bool
	onMatch  func() tea.Cmd

	// preview is the request body the command acts with, nil for none.
	preview *bodyPreview
}

func newTypeNameDialog(st styles, question, detail, name string, command shellCommand, onMatch func() tea.Cmd) *typeNameDialog {
	in := textinput.New()
	in.Prompt = "> "
	in.SetStyles(st.input)
	in.Focus()
	return &typeNameDialog{
		question: question,
		detail:   detail,
		name:     name,
		command:  command,
		input:    in,
		onMatch:  onMatch,
	}
}

func (d *typeNameDialog) update(msg tea.Msg) (bool, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyPressMsg)
	switch {
	case !isKey:
		// A pasted name is typed, not submitted: enter is still the user's to press.
	case key.Matches(keyMsg, cancelKey):
		return true, nil
	case key.Matches(keyMsg, submitKey):
		if d.input.Value() != d.name {
			d.mismatch = true
			return false, nil
		}
		return true, d.onMatch()
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	d.mismatch = false
	return false, cmd
}

func (d *typeNameDialog) view(st styles, width, height int) string {
	// A project name comes from the config file, which may have been edited by hand.
	top := []string{st.title.Render(output.SanitizeCell(d.question))}
	for _, note := range d.notes {
		top = append(top, st.warnText.Render(output.SanitizeCell(note)))
	}
	if d.detail != "" {
		top = append(top, output.SanitizeCell(d.detail))
	}
	bottom := []string{"", "Type " + output.SanitizeCell(d.name) + " to confirm.", d.input.View()}
	if d.mismatch {
		what := d.what
		if what == "" {
			what = "name"
		}
		bottom = append(bottom, st.errorText.Render("That is not the "+what+"; nothing was sent."))
	}
	bottom = append(bottom, "", st.dim.Render("runs: "+output.SanitizeCell(d.command.String())), "", "enter confirm · esc cancel")
	return framed(st, width, height, top, d.preview, bottom)
}

func (d *typeNameDialog) bindings() []key.Binding { return []key.Binding{submitKey, cancelKey} }

func (d *typeNameDialog) equivalent() shellCommand { return d.command }

// framed draws a dialog's lines in its box, with the start of the body it is
// about between top and bottom: as many lines of it as keep the whole dialog
// within height rows, so that the answer keys are never cut off.
func framed(st styles, width, height int, top []string, p *bodyPreview, bottom []string) string {
	box := st.dialog.Width(dialogWidth(width))
	draw := func(rows []string) string {
		return box.Render(strings.Join(slices.Concat(top, rows, bottom), "\n"))
	}
	if p == nil {
		return draw(nil)
	}
	inner := dialogWidth(width) - box.GetHorizontalFrameSize()
	room := previewLines
	if height > 0 {
		room = min(room, max(height-lipgloss.Height(draw(p.rows(st, inner, 0))), 0))
	}
	return draw(p.rows(st, inner, room))
}

// previewLines is as much of a body as a question shows: enough to see what it
// is, not so much that the question scrolls away.
const previewLines = 12

// bodyPreview is the start of a request body, as a question about sending it
// shows it.
type bodyPreview struct {
	// source says where the body came from, "" for the form's own.
	source string

	head  []string
	lines int
	size  int
}

// newBodyPreview reads body once, when the question is asked: a body can run to
// megabytes, and the question is drawn on every frame.
func newBodyPreview(body []byte, source string) *bodyPreview {
	text := prettyBody(body)
	head := strings.SplitN(text, "\n", previewLines+1)
	return &bodyPreview{
		source: source,
		head:   head[:min(len(head), previewLines)],
		lines:  strings.Count(text, "\n") + 1,
		size:   len(body),
	}
}

// rows is the preview with at most room of the body's lines, each cut to width.
func (p *bodyPreview) rows(st styles, width, room int) []string {
	title := fmt.Sprintf("The body, %d bytes", p.size)
	if p.source != "" {
		title += ", from the " + p.source
	}
	rows := []string{"", wrap(st.dim.Render(output.SanitizeCell(title)+":"), width)}
	shown := min(room, len(p.head))
	for _, line := range p.head[:shown] {
		rows = append(rows, clip(line, width))
	}
	switch more := p.lines - shown; {
	case more == 1:
		rows = append(rows, st.dim.Render("… 1 more line"))
	case more > 1:
		rows = append(rows, st.dim.Render(fmt.Sprintf("… %d more lines", more)))
	}
	return rows
}

// dialogWidth keeps a dialog readable: as wide as the text wants on a small
// terminal, and not stretched across a large one.
func dialogWidth(width int) int {
	if width <= 0 {
		return 72
	}
	return min(width, 72)
}
