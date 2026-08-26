package output

import "strings"

// Sanitize strips the control bytes a terminal would act on rather than draw,
// from a string that came out of a file or the network rather than off a
// keyboard.
//
// A field like a template's description or an entity's name is data, not
// something fft composed, and printing it verbatim hands whoever wrote that
// file the cursor: a bare ESC can start a CSI sequence, a bare CR can overwrite
// the line above it, a BEL can ring the bell. None of that requires the sgr
// colour sequences [width] already strips for measurement — this is the
// broader "never let an untrusted string steer the terminal" rule, applied
// before the string is printed at all. Tab and newline survive: multi-line
// text stays multi-line.
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n':
			return r
		case r < 0x20 || r == 0x7f:
			return -1
		case r >= 0x80 && r <= 0x9f:
			return -1
		default:
			return r
		}
	}, s)
}

// SanitizeCell is [Sanitize] for a string going into one cell of a table or one
// entry of a numbered list — anywhere the layout is one record per line.
//
// The tab and newline Sanitize keeps are exactly what such a layout cannot
// survive: a newline in a cell is not multi-line text, it is a row the printer
// never counted and the reader cannot tell from a real one, which is how a
// crafted description forges a table row on a stdout that is supposed to be
// only data. A tab is the column separator itself. Both fold to a space, so the
// value stays inside the cell it was printed in.
func SanitizeCell(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' {
			return ' '
		}
		return r
	}, Sanitize(s))
}
