package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// dialog is a question that has the keyboard until it is answered.
type dialog interface {
	// update handles a key. It returns whether the dialog is finished, and what
	// answering it started.
	update(msg tea.KeyPressMsg) (finished bool, cmd tea.Cmd)
	view(st styles, width int) string
	bindings() []key.Binding

	// equivalent is the command a yes would run, "" when a yes runs nothing yet.
	equivalent() string
}

var (
	yesKey    = key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "yes"))
	noKey     = key.NewBinding(key.WithKeys("n", "N", "esc", "enter"), key.WithHelp("n/esc", "no"))
	submitKey = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm"))
	cancelKey = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
)

// confirmDialog asks a yes/no question. No is the default: enter declines, and
// only y goes ahead.
type confirmDialog struct {
	question string
	detail   string
	command  string
	onYes    func() tea.Cmd
}

func (d *confirmDialog) update(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch {
	case key.Matches(msg, yesKey):
		return true, d.onYes()
	case key.Matches(msg, noKey):
		return true, nil
	}
	return false, nil
}

func (d *confirmDialog) view(st styles, width int) string {
	lines := []string{st.title.Render(d.question)}
	if d.detail != "" {
		lines = append(lines, d.detail)
	}
	if d.command != "" {
		lines = append(lines, "", st.dim.Render("runs: "+d.command))
	}
	lines = append(lines, "", "y yes · n no")
	return st.dialog.Width(dialogWidth(width)).Render(strings.Join(lines, "\n"))
}

func (d *confirmDialog) bindings() []key.Binding { return []key.Binding{yesKey, noKey} }

func (d *confirmDialog) equivalent() string { return d.command }

// typeNameDialog asks for a name to be typed back before something that cannot be
// undone. A y is too easy to give by accident; the name of the thing is not.
type typeNameDialog struct {
	question string
	name     string
	command  string
	input    textinput.Model
	mismatch bool
	onMatch  func() tea.Cmd
}

func newTypeNameDialog(st styles, question, name, command string, onMatch func() tea.Cmd) *typeNameDialog {
	in := textinput.New()
	in.Prompt = "> "
	in.SetStyles(st.input)
	in.Focus()
	return &typeNameDialog{question: question, name: name, command: command, input: in, onMatch: onMatch}
}

func (d *typeNameDialog) update(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch {
	case key.Matches(msg, cancelKey):
		return true, nil
	case key.Matches(msg, submitKey):
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
	lines := []string{
		st.title.Render(d.question),
		"Type " + d.name + " to confirm.",
		d.input.View(),
	}
	if d.mismatch {
		lines = append(lines, st.errorText.Render("That is not the name; nothing was removed."))
	}
	lines = append(lines, "", st.dim.Render("runs: "+d.command), "", "enter confirm · esc cancel")
	return st.dialog.Width(dialogWidth(width)).Render(strings.Join(lines, "\n"))
}

func (d *typeNameDialog) bindings() []key.Binding { return []key.Binding{submitKey, cancelKey} }

func (d *typeNameDialog) equivalent() string { return d.command }

// dialogWidth keeps a dialog readable: as wide as the text wants on a small
// terminal, and not stretched across a large one.
func dialogWidth(width int) int {
	if width <= 0 {
		return 72
	}
	return min(width, 72)
}
