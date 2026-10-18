package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// tableRows lays cells out in aligned columns under the header in cells[0],
// marking the row at cursor (an index into the rows after the header) as selected.
// The last column is never padded, so its width does not matter.
func tableRows(st styles, cells [][]string, cursor, width int) []string {
	widths := make([]int, len(cells[0]))
	for _, row := range cells {
		for i, cell := range row {
			widths[i] = max(widths[i], ansi.StringWidth(cell))
		}
	}

	lines := make([]string, 0, len(cells))
	for i, row := range cells {
		var b strings.Builder
		for j, cell := range row {
			b.WriteString(cell)
			if j < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-ansi.StringWidth(cell)+2))
			}
		}
		line := b.String()
		switch {
		case i == 0:
			line = st.dim.Render("  " + line)
		case i-1 == cursor:
			line = st.selected.Render("> " + line)
		default:
			line = "  " + line
		}
		lines = append(lines, clip(line, width))
	}
	return lines
}

// window is at most room of lines, starting scroll lines in, and where it starts.
// That is less than scroll when the lines have grown fewer or the terminal taller
// since, so that the window never ends in empty rows a scroll back would fill.
func window(lines []string, scroll, room int) (shown []string, first int) {
	first = min(scroll, max(len(lines)-room, 0))
	return lines[first:min(first+room, len(lines))], first
}

// pick is the item at *cursor, after moving the cursor back onto items if they
// have shrunk under it. It is false, and leaves the cursor alone, when there are
// no items.
func pick[T any](items []T, cursor *int) (T, bool) {
	if len(items) == 0 {
		var zero T
		return zero, false
	}
	*cursor = min(max(*cursor, 0), len(items)-1)
	return items[*cursor], true
}

// clip cuts s to width terminal cells. A width of zero or less means the width is
// not known yet, and s is left alone.
func clip(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// wrap breaks s to width cells, and leaves it alone while the width is unknown.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Wrap(s, width, "")
}

// trimPaste is msg with the line breaks a paste ends in cut off, and any other
// msg as it is.
func trimPaste(msg tea.Msg) tea.Msg {
	if paste, isPaste := msg.(tea.PasteMsg); isPaste {
		paste.Content = strings.TrimRight(paste.Content, "\r\n")
		return paste
	}
	return msg
}
