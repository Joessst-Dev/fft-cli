package tui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// styles is every look the UI uses, in one place, so that switching colour off is
// one decision rather than one per widget.
type styles struct {
	color bool

	tab       lipgloss.Style
	activeTab lipgloss.Style
	title     lipgloss.Style
	dim       lipgloss.Style
	selected  lipgloss.Style
	errorText lipgloss.Style
	okText    lipgloss.Style
	warnText  lipgloss.Style
	statusBar lipgloss.Style
	badge     lipgloss.Style
	dialog    lipgloss.Style
	panel     lipgloss.Style

	help  help.Styles
	input textinput.Styles
}

// newStyles returns the UI's styles. Without colour every style is plain, and
// nothing the UI means is carried by colour alone: the selected row has a marker,
// a failure says "failed", and the read-only badge is a word.
func newStyles(color bool) styles {
	// Borders are box-drawing characters, not colour, so they stay: a dialog is
	// still visibly a dialog on a terminal with NO_COLOR.
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

	if !color {
		plain := lipgloss.NewStyle()
		return styles{
			tab:       plain.Padding(0, 1),
			activeTab: plain.Padding(0, 1),
			title:     plain,
			dim:       plain,
			selected:  plain,
			errorText: plain,
			okText:    plain,
			warnText:  plain,
			statusBar: plain,
			badge:     plain,
			dialog:    box,
			panel:     box,
			help:      help.Styles{},
			input:     plainInputStyles(),
		}
	}

	accent := lipgloss.Color("#7D56F4")
	muted := lipgloss.Color("#8A8A8A")
	return styles{
		color:     true,
		tab:       lipgloss.NewStyle().Padding(0, 1).Foreground(muted),
		activeTab: lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent),
		title:     lipgloss.NewStyle().Bold(true),
		dim:       lipgloss.NewStyle().Foreground(muted),
		selected:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		errorText: lipgloss.NewStyle().Foreground(lipgloss.Color("#E5484D")),
		okText:    lipgloss.NewStyle().Foreground(lipgloss.Color("#46A758")),
		warnText:  lipgloss.NewStyle().Foreground(lipgloss.Color("#F5A524")),
		statusBar: lipgloss.NewStyle().Foreground(lipgloss.Color("#DDDDDD")).Background(lipgloss.Color("#303030")),
		badge:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#E5484D")).Padding(0, 1),
		dialog:    box.BorderForeground(accent),
		panel:     box.BorderForeground(muted),
		help:      help.DefaultDarkStyles(),
		input:     steadyCursor(textinput.DefaultDarkStyles()),
	}
}

// steadyCursor stops the text cursor blinking: a blink is a timer message several
// times a second for as long as a field has focus, for no information at all.
func steadyCursor(s textinput.Styles) textinput.Styles {
	s.Cursor.Blink = false
	return s
}

func plainInputStyles() textinput.Styles {
	s := textinput.DefaultDarkStyles()
	plain := textinput.StyleState{}
	s.Focused = plain
	s.Blurred = plain
	s.Cursor.Color = nil
	return steadyCursor(s)
}
