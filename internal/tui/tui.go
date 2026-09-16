package tui

import (
	"context"
	"errors"
	"io"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Options is what [Run] needs from its caller.
type Options struct {
	// Runner executes the commands the UI builds.
	Runner Runner

	// In is where keystrokes come from; it must be a terminal.
	In io.Reader

	// Out is where the UI is drawn. fft passes stderr, so that stdout stays what
	// it is everywhere else: data, and here nothing at all.
	Out io.Writer
}

// Run shows the UI until the user quits or ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	if opts.Runner == nil {
		return errors.New("tui: no runner to execute commands with")
	}
	p := tea.NewProgram(newModel(opts),
		tea.WithContext(ctx),
		tea.WithInput(opts.In),
		tea.WithOutput(opts.Out),
	)
	_, err := p.Run()
	return err
}

type keyMap struct {
	quit key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

type model struct {
	keys  keyMap
	frame lipgloss.Style
}

func newModel(Options) model {
	return model{
		keys:  defaultKeys(),
		frame: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && key.Matches(k, m.keys.quit) {
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() tea.View {
	help := m.keys.quit.Help()
	v := tea.NewView(m.frame.Render("fft — interactive mode\n\n" + help.Key + "  " + help.Desc))
	// The alternate screen gives the shell's scrollback back untouched on exit.
	v.AltScreen = true
	return v
}
