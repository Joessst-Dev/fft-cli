package tui

import (
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

	hint  hintStyles
	input textinput.Styles
}

// hintStyles is the look of the key hint at the bottom. It is the one part of the
// UI drawn for the terminal's own background, since it must be seen at a glance
// on light and dark terminals alike: bubbles' help styles are a grey that fades
// into either.
type hintStyles struct {
	key  lipgloss.Style
	desc lipgloss.Style
	sep  lipgloss.Style

	// rule is the line between the status bar and the hint.
	rule lipgloss.Style
}

// newHintStyles returns the hint's styles for a dark or a light background.
// Without colour they are plain, and the rule above the hint and the separators
// between its keys are what set it apart.
func newHintStyles(color, dark bool) hintStyles {
	if !color {
		return hintStyles{}
	}
	pick := lipgloss.LightDark(dark)
	muted := pick(lipgloss.Color("#6E6E6E"), lipgloss.Color("#8A8A8A"))
	return hintStyles{
		key:  lipgloss.NewStyle().Bold(true).Foreground(pick(lipgloss.Color("#5B3CC4"), lipgloss.Color("#A78BFA"))),
		desc: lipgloss.NewStyle(),
		sep:  lipgloss.NewStyle().Foreground(muted),
		rule: lipgloss.NewStyle().Foreground(muted),
	}
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
			hint:      newHintStyles(false, true),
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
		// Dark until the terminal says otherwise: it is the more common background,
		// and the palette above is chosen for it.
		hint:  newHintStyles(true, true),
		input: steadyCursor(textinput.DefaultDarkStyles()),
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
