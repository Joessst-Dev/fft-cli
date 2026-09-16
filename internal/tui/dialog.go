package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// dialog is a question that has the keyboard until it is answered.
type dialog interface {
	// update handles a key or a paste. It returns whether the dialog is finished,
	// and what answering it started.
	update(msg tea.Msg) (finished bool, cmd tea.Cmd)
	view(st styles, width int) string
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
	detail   string
	command  shellCommand
	onYes    func() tea.Cmd
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

func (d *confirmDialog) view(st styles, width int) string {
	lines := []string{st.title.Render(output.SanitizeCell(d.question))}
	if d.detail != "" {
		lines = append(lines, output.SanitizeCell(d.detail))
	}
	if !d.command.empty() {
		lines = append(lines, "", st.dim.Render("runs: "+output.SanitizeCell(d.command.String())))
	}
	lines = append(lines, "", "y yes · n no")
	return st.dialog.Width(dialogWidth(width)).Render(strings.Join(lines, "\n"))
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

	command  shellCommand
	input    textinput.Model
	mismatch bool
	onMatch  func() tea.Cmd
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

func (d *typeNameDialog) view(st styles, width int) string {
	// A project name comes from the config file, which may have been edited by hand.
	lines := []string{st.title.Render(output.SanitizeCell(d.question))}
	if d.detail != "" {
		lines = append(lines, output.SanitizeCell(d.detail))
	}
	lines = append(lines, "Type "+output.SanitizeCell(d.name)+" to confirm.", d.input.View())
	if d.mismatch {
		what := d.what
		if what == "" {
			what = "name"
		}
		lines = append(lines, st.errorText.Render("That is not the "+what+"; nothing was sent."))
	}
	lines = append(lines, "", st.dim.Render("runs: "+output.SanitizeCell(d.command.String())), "", "enter confirm · esc cancel")
	return st.dialog.Width(dialogWidth(width)).Render(strings.Join(lines, "\n"))
}

func (d *typeNameDialog) bindings() []key.Binding { return []key.Binding{submitKey, cancelKey} }

func (d *typeNameDialog) equivalent() shellCommand { return d.command }

// dialogWidth keeps a dialog readable: as wide as the text wants on a small
// terminal, and not stretched across a large one.
func dialogWidth(width int) int {
	if width <= 0 {
		return 72
	}
	return min(width, 72)
}
