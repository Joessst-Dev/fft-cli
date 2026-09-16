package tui

import "charm.land/bubbles/v2/key"

// globalKeys work on every screen, unless a dialog or a text field has the
// keyboard — then only quit does, so that typing a "q" into a field types a "q".
type globalKeys struct {
	screens  []key.Binding
	next     key.Binding
	prev     key.Binding
	projects key.Binding
	help     key.Binding
	runs     key.Binding
	copy     key.Binding
	quit     key.Binding
	forceQ   key.Binding
}

func newGlobalKeys() globalKeys {
	screens := make([]key.Binding, len(screenNames))
	for i := range screenNames {
		n := string(rune('1' + i))
		screens[i] = key.NewBinding(key.WithKeys(n))
	}
	return globalKeys{
		screens:  screens,
		next:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("1-7/tab", "switch screen")),
		prev:     key.NewBinding(key.WithKeys("shift+tab")),
		projects: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "switch project")),
		help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		runs:     key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "running commands")),
		copy:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy command")),
		quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		forceQ:   key.NewBinding(key.WithKeys("ctrl+c")),
	}
}

func (k globalKeys) bindings() []key.Binding {
	return []key.Binding{k.next, k.projects, k.runs, k.copy, k.help, k.quit}
}

// helpKeys is what the help line shows: the focused part's keys first, then the
// global ones. It implements help.KeyMap.
type helpKeys struct {
	local  []key.Binding
	global []key.Binding
}

func (h helpKeys) ShortHelp() []key.Binding {
	return append(append([]key.Binding{}, h.local...), h.global...)
}

func (h helpKeys) FullHelp() [][]key.Binding {
	if len(h.local) == 0 {
		return [][]key.Binding{h.global}
	}
	return [][]key.Binding{h.local, h.global}
}
