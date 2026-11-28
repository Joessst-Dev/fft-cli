// Package shellquote spells a value for a command line that fft prints for a
// user to paste into a shell. Anything that reaches such a line from the tenant
// goes through it: a facility id or a tag value is text a stranger may have chosen.
package shellquote

import (
	"strings"
	"unicode"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// Quote is s quoted so that a POSIX shell, fish and PowerShell all read it back
// as s, and leaves it bare when none of them would read it as anything else.
//
// Single quotes are the one quoting the three share, and only while the value
// holds no single quote and no backslash: a POSIX shell cannot escape a quote
// inside them, fish reads \' and \\ there, and PowerShell doubles a quote and
// takes the typographic ones for quotes too. A value with any of those, or with a
// control or formatting character, has no portable spelling. It is returned
// POSIX-quoted and stripped of what a terminal would act on, for display only,
// and portable is false.
func Quote(s string) (quoted string, portable bool) {
	switch {
	case s == "":
		return "''", true
	case !needsQuoting(s):
		return s, true
	case strings.ContainsFunc(s, unquotable):
		return "'" + strings.ReplaceAll(output.SanitizeCell(s), "'", `'\''`) + "'", false
	default:
		return "'" + s + "'", true
	}
}

// needsQuoting reports whether some shell would read s bare as anything but s.
//
// The bare alphabet leaves out what any of the three treats specially: @ and , are
// PowerShell's splatting and array operators, and % starts its stop-parsing token.
// PowerShell also reads a bare token that looks like a number as one — 1kb is
// 1024, 0x10 is 16, 1.50 is 1.5 — so a token that starts like a number is quoted,
// unless it is a plain integer that reads back unchanged.
func needsQuoting(s string) bool {
	if strings.ContainsFunc(s, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		default:
			return !strings.ContainsRune("-_./:+=", r)
		}
	}) {
		return true
	}
	return numberLike(s) && !plainInteger(s)
}

// numberLike reports whether s starts the way a PowerShell numeric literal does.
func numberLike(s string) bool {
	s = strings.TrimLeft(s, "+-")
	s = strings.TrimPrefix(s, ".")
	return s != "" && s[0] >= '0' && s[0] <= '9'
}

// plainInteger reports whether s is digits with no leading zero, which every
// shell passes on exactly as written.
func plainInteger(s string) bool {
	if s == "0" {
		return true
	}
	if s == "" || s[0] == '0' {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return r < '0' || r > '9' })
}

// unquotable reports whether r has no spelling inside single quotes that every
// shell agrees on.
func unquotable(r rune) bool {
	switch r {
	case '\'', '\\', '‘', '’', '‚', '‛':
		return true
	}
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}
