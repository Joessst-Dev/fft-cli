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
		if IsFenceDelimiter(line) {
			inFence = !inFence
			continue
		}
		if !inFence {
			lines[i] = escapeLine(line)
		}
	}
	return strings.Join(lines, "\n")
}

// IsFenceDelimiter reports whether a line opens or closes a fenced code block —
// a (possibly indented) run of at least three backticks or tildes. Both this
// package and tools/docsgen walk Markdown line-by-line tracking fence state, and
// must agree on what a fence looks like or one of them will silently process
// text the other treats as code.
func IsFenceDelimiter(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// escapeLine escapes '<' outside inline code spans on a single line.
//
// A code span opens on a run of one or more backticks and closes on the next
// run of the same length — the CommonMark rule that lets a span delimited by
// two backticks wrap content containing a literal single backtick. Toggling on
// every single backtick would read that opening run as open-then-immediately-
// close, leaving the content in between unprotected.
func escapeLine(line string) string {
	var b strings.Builder
	runes := []rune(line)
	inCode := false
	codeDelim := 0
	for i := 0; i < len(runes); {
		if runes[i] == '`' {
			j := i
			for j < len(runes) && runes[j] == '`' {
				j++
			}
			run := j - i
			b.WriteString(string(runes[i:j]))
			switch {
			case !inCode:
				inCode = true
				codeDelim = run
			case run == codeDelim:
				inCode = false
				codeDelim = 0
			}
			i = j
			continue
		}
		if runes[i] == '<' && !inCode {
			b.WriteString("&lt;")
		} else {
			b.WriteRune(runes[i])
		}
		i++
	}
	return b.String()
}
