package tui

import "strings"

// commandLine renders args as the fft command a user would type in a shell to do
// what the UI is about to do. It is what the status bar shows and what y copies.
func commandLine(args []string) string {
	var b strings.Builder
	b.WriteString("fft")
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

// shellQuote quotes s for a POSIX shell, and leaves it alone when no shell would
// read it as anything but itself.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, needsQuoting) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func needsQuoting(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	case strings.ContainsRune("-_./:@%+=,", r):
		return false
	default:
		return true
	}
}
