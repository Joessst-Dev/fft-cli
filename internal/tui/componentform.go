package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

type installPromptKeys struct {
	cancel key.Binding
}

func newInstallPromptKeys() installPromptKeys {
	return installPromptKeys{
		cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// installPrompt asks what to install: one field, because `fft component install`
// takes one thing — a name, an owner/repo[@version], or a directory.
//
// It is not built on [addForm], which collects the eleven flags of `project add`.
// One line does not need a focus ring, a toggle or a submit key of its own.
type installPrompt struct {
	input textinput.Model
	keys  installPromptKeys
	enter key.Binding

	// problem is what the prompt itself found wrong, before anything ran: the
	// prompt closes as the install is sent, so what the command says about it is
	// the screen's to show, not this form's.
	problem string
}

func newInstallPrompt(st styles, initial string) *installPrompt {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "emulator, owner/repo@v1.2.3, or ./my-component"
	in.SetWidth(formInputWidth)
	in.SetStyles(st.input)
	in.SetValue(initial)
	in.Focus()
	return &installPrompt{
		input: in,
		keys:  newInstallPromptKeys(),
		enter: newComponentKeys().submit,
	}
}

func (p *installPrompt) value() string { return strings.TrimSpace(p.input.Value()) }

func (p *installPrompt) update(msg tea.Msg) (formEvent, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, p.keys.cancel):
			return formCancelled, nil
		case key.Matches(keyMsg, p.enter):
			return formSubmitted, nil
		}
	}
	in, cmd := p.input.Update(msg)
	p.input = in
	return formEditing, cmd
}

func (p *installPrompt) bindings() []key.Binding {
	return []key.Binding{p.enter, p.keys.cancel}
}

func (p *installPrompt) view(st styles, width int) string {
	lines := []string{
		st.title.Render("Install a component"),
		"",
		st.dim.Render("Name one fft ships (emulator), an owner/repo[@version] on GitHub, or a"),
		st.dim.Render("directory to install from — spelled as a path: ./x, ../x, /x or ~/x."),
		"",
		"Source  " + p.input.View(),
		"",
	}
	if p.problem != "" {
		lines = append(lines, wrap(st.warnText.Render(output.SanitizeCell(p.problem)), width))
	}
	return strings.Join(lines, "\n")
}
