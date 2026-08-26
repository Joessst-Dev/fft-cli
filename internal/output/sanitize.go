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
