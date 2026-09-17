package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// legendEntry is one key of the legend and what it does.
type legendEntry struct {
	// of is the binding the entry explains, so that a spec can tell a binding the
	// legend forgot. It is the zero binding for a key a bubbles component handles
	// on the screen's behalf, such as the list's paging.
	of key.Binding

	// keys is how the entry spells its keys; of's help key when empty.
	keys string
	desc string
}

func (e legendEntry) label() string {
	if e.keys != "" {
		return e.keys
	}
	return e.of.Help().Key
}

// legendSection is a group of keys that work together: those of a screen, or of
// one of its modes.
type legendSection struct {
	// title heads the section; a screen's main section has none, since the
	// legend's own title names the screen.
	title string

	// note is a line about the section as the screen stands, such as keys that do
	// nothing for the operation on display.
	note string

	entries []legendEntry

	// compact draws the entries as a run of "keys what" items, for a mode whose
	// keys are few and short. columns draws them two to a row where they fit.
	compact bool
	columns bool
}

// legendLines draws sections width columns wide, 0 for a width nobody knows.
// The key column is as wide as the widest key of any tabular section, so that
// the descriptions line up down the whole legend.
func legendLines(st styles, width int, sections []legendSection) []string {
	keyWidth := 0
	for _, sec := range sections {
		if sec.compact {
			continue
		}
		for _, e := range sec.entries {
			keyWidth = max(keyWidth, ansi.StringWidth(e.label()))
		}
	}

	var lines []string
	for _, sec := range sections {
		if sec.title != "" {
			lines = append(lines, " "+st.title.Render(sec.title))
		}
		if sec.note != "" {
			lines = append(lines, "  "+st.dim.Render(output.SanitizeCell(sec.note)))
		}
		switch {
		case sec.compact:
			lines = append(lines, compactLines(st, width, sec.entries)...)
		default:
			lines = append(lines, tableLines(st, width, keyWidth, sec)...)
		}
	}
	for i, line := range lines {
		lines[i] = clip(line, width)
	}
	return lines
}

const legendIndent = "  "

// legendGap is the space between a key and what it does, and between two columns.
const legendGap = "  "

func compactLines(st styles, width int, entries []legendEntry) []string {
	items := make([]string, 0, len(entries))
	for _, e := range entries {
		items = append(items, st.selected.Render(e.label())+" "+e.desc)
	}
	packed := pack(items, " · ", max(width-len(legendIndent), 0))
	for i, line := range packed {
		packed[i] = legendIndent + line
	}
	return packed
}

func tableLines(st styles, width, keyWidth int, sec legendSection) []string {
	cells := make([]string, 0, len(sec.entries))
	descWidth := 0
	for _, e := range sec.entries {
		descWidth = max(descWidth, ansi.StringWidth(e.desc))
	}
	for _, e := range sec.entries {
		label := e.label()
		cell := st.selected.Render(label) + strings.Repeat(" ", keyWidth-ansi.StringWidth(label)) + legendGap + e.desc
		cells = append(cells, cell)
	}

	cellWidth := keyWidth + len(legendGap) + descWidth
	twoFit := width <= 0 || len(legendIndent)+2*cellWidth+len(legendGap) <= width
	if !sec.columns || !twoFit {
		lines := make([]string, len(cells))
		for i, c := range cells {
			lines[i] = legendIndent + c
		}
		return lines
	}

	lines := make([]string, 0, (len(cells)+1)/2)
	for i := 0; i < len(cells); i += 2 {
		line := legendIndent + cells[i]
		if i+1 < len(cells) {
			line += strings.Repeat(" ", cellWidth-ansi.StringWidth(cells[i])) + legendGap + cells[i+1]
		}
		lines = append(lines, line)
	}
	return lines
}

// legendMinRows is the fewest rows the legend is drawn in under the tab bar: its
// title and a couple of keys. A terminal with less room below the chrome gives the
// legend all of its rows instead.
const legendMinRows = 4

// legendSections is everything the legend lists for the screen on display.
func (m *app) legendSections() []legendSection {
	return slices.Concat(m.screens[m.current].legend(), []legendSection{
		m.panel.legend(),
		questionLegend(),
		m.keys.legend(),
		m.legendKeys.legend(),
	})
}

// legendHeight is how many rows the open legend is drawn in.
func (m *app) legendHeight() int {
	rows := m.bodyHeight(m.hintLine())
	if m.height > 0 && rows < legendMinRows {
		return m.height
	}
	return rows
}

// legendView draws the legend within height rows, scrolled as far as the user
// scrolled it; a height of 0 is one nobody knows, and draws all of it.
func (m *app) legendView(height int) string {
	lines := legendLines(m.st, m.width, m.legendSections())
	title := " " + m.st.title.Render("Keys · "+screenNames[m.current])
	hint := "? or esc closes"
	room := height - 1
	if height > 0 && len(lines) > room {
		hint = "↑/↓ scroll · " + hint
	}
	title = clip(title+"   "+m.st.dim.Render(hint), m.width)
	if height <= 0 {
		return strings.Join(append([]string{title}, lines...), "\n")
	}
	if room <= 0 {
		return title
	}

	// A terminal that grew since the last scroll has less to scroll through.
	scroll := min(m.legendScroll, max(len(lines)-room, 0))
	shown := lines[scroll:min(scroll+room, len(lines))]
	if scroll+room < len(lines) {
		shown[len(shown)-1] = legendIndent + m.st.dim.Render("↓ more")
	}
	return strings.Join(append([]string{title}, shown...), "\n")
}

// scrollLegend moves the legend by rows, within what there is to show.
func (m *app) scrollLegend(rows int) {
	room := m.legendHeight() - 1
	total := len(legendLines(m.st, m.width, m.legendSections()))
	m.legendScroll = min(max(m.legendScroll+rows, 0), max(total-room, 0))
}
