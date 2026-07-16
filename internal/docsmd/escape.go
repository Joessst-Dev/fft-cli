// Package docsmd holds the small Markdown transforms shared by the two
// documentation-site generators — tools/docsgen (guide pages from the skill and
// README) and fft gen-docs (the CLI reference).
package docsmd

import "strings"

// EscapeAngles escapes a bare '<' (to &lt;) that sits outside fenced and inline
// code, so a placeholder like <id> in prose is not parsed as an HTML tag.
//
// VitePress compiles rendered Markdown as a Vue template, and a stray "<id>" reads
// as an element with no closing tag — a hard build error. Only '<' opens a tag, so
// only '<' is touched: '>' is left alone, both because it opens nothing and because
// escaping it would mangle a blockquote marker. Code is left untouched too — there
// Markdown already escapes the angle brackets, and escaping again would show the
// entities raw.
func EscapeAngles(md string) string {
	lines := strings.Split(md, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence {
			lines[i] = escapeLine(line)
		}
	}
	return strings.Join(lines, "\n")
}

// escapeLine escapes '<' outside inline code spans on a single line.
func escapeLine(line string) string {
	var b strings.Builder
	inCode := false
	for _, r := range line {
		switch {
		case r == '`':
			inCode = !inCode
			b.WriteRune(r)
		case r == '<' && !inCode:
			b.WriteString("&lt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
