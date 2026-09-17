package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

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
		next:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("1-7/tab", "screens")),
		prev:     key.NewBinding(key.WithKeys("shift+tab")),
		projects: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "project")),
		help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "all keys")),
		runs:     key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "running")),
		copy:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy")),
		quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		forceQ:   key.NewBinding(key.WithKeys("ctrl+c")),
	}
}

// bindings is the global part of the hint line. The legend's key comes last, so
// that however the line wraps, the way to every other key is where the eye ends.
func (k globalKeys) bindings() []key.Binding {
	return []key.Binding{k.next, k.projects, k.runs, k.copy, k.quit, k.help}
}

// legend is what the global keys do, in the words the legend has room for.
func (k globalKeys) legend() legendSection {
	return legendSection{
		title:   "Everywhere",
		columns: true,
		entries: []legendEntry{
			{of: k.next, keys: "1-7, tab", desc: "switch screen"},
			{of: k.prev, keys: "shift+tab", desc: "previous screen"},
			{of: k.projects, desc: "go to Projects"},
			{of: k.runs, desc: "running commands"},
			{of: k.copy, desc: "copy the fft command"},
			{of: k.quit, keys: "q, ctrl+c", desc: "quit"},
			{of: k.help, desc: "open or close this legend"},
		},
	}
}

// legendKeys are the keys of the open legend. Nothing else on the screen
// underneath takes a key while it is open.
type legendKeys struct {
	up     key.Binding
	down   key.Binding
	pageUp key.Binding
	pageDn key.Binding
	close  key.Binding
}

func newLegendKeys() legendKeys {
	return legendKeys{
		up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "scroll")),
		down:   key.NewBinding(key.WithKeys("down", "j")),
		pageUp: key.NewBinding(key.WithKeys("pgup")),
		pageDn: key.NewBinding(key.WithKeys("pgdown")),
		close:  key.NewBinding(key.WithKeys("?", "esc"), key.WithHelp("?/esc", "close")),
	}
}

func (k legendKeys) legend() legendSection {
	return legendSection{
		title:   "In this legend",
		compact: true,
		entries: []legendEntry{
			{of: k.up, keys: "↑/↓, j/k, pgup/pgdn", desc: "scroll"},
			{of: k.close, keys: "?, esc", desc: "close"},
		},
	}
}

// helpKeys is what the hint line shows: the focused part's keys, then the global
// ones.
type helpKeys struct {
	local  []key.Binding
	global []key.Binding
}

// hintSeparator goes between two keys on a hint line.
const hintSeparator = " • "

// hintView draws h on as many lines as its keys need at width columns: the local
// keys from a line of their own, the global ones from the next. Nothing is cut
// short with an ellipsis, so the last global key — the legend's — is always shown,
// and it is drawn in the key's colour throughout, to be found first. A width of 0
// is one nobody knows, and gives each part a single line.
func hintView(st styles, width int, h helpKeys) string {
	hs := st.hint
	var lines []string
	for part, bindings := range [][]key.Binding{h.local, h.global} {
		items := make([]string, 0, len(bindings))
		for i, b := range bindings {
			help := b.Help()
			if !b.Enabled() || help.Key == "" {
				continue
			}
			desc := hs.desc
			if part == 1 && i == len(bindings)-1 {
				desc = hs.key.Bold(false)
			}
			items = append(items, hs.key.Render(help.Key)+" "+desc.Render(help.Desc))
		}
		lines = append(lines, pack(items, hs.sep.Render(hintSeparator), width)...)
	}
	return strings.Join(lines, "\n")
}

// pack joins items with sep into lines no wider than width, starting a new line
// rather than splitting an item. An item wider than a line on its own is cut.
func pack(items []string, sep string, width int) []string {
	var lines []string
	var line strings.Builder
	used := 0
	for _, item := range items {
		w := ansi.StringWidth(item)
		if used > 0 && (width <= 0 || used+ansi.StringWidth(sep)+w <= width) {
			line.WriteString(sep)
			line.WriteString(item)
			used += ansi.StringWidth(sep) + w
			continue
		}
		if used > 0 {
			lines = append(lines, line.String())
			line.Reset()
		}
		line.WriteString(clip(item, width))
		used = w
		if width > 0 {
			used = min(w, width)
		}
	}
	if used > 0 {
		lines = append(lines, line.String())
	}
	return lines
}
