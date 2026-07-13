package output

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// gap is the number of spaces between two columns.
const gap = 3

// Rows is a rendered table: a header and its rows, already stringified.
//
// A cell may carry colour, because [width] measures what a terminal will
// actually show rather than how many bytes it took to say it.
type Rows struct {
	Headers []string
	Rows    [][]string
}

// sgr matches an ANSI colour sequence: the bytes a terminal consumes and does
// not draw.
var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

// width is how many columns a cell occupies on screen.
//
// text/tabwriter cannot do this. It measures cells in runes and counts the ten
// bytes of a colour escape as ten columns, so one coloured cell shifts every
// column to its right — and tabwriter.Escape does not help: the escaped text is
// still counted. Colour and tabwriter are simply incompatible, which is why this
// package pads its own columns.
//
// A double-width rune (CJK, an emoji) still counts as one. No field the API
// returns is plausibly in that alphabet, and guessing wrong there costs a
// misaligned column, not a wrong answer.
func width(cell string) int {
	return utf8.RuneCountInString(sgr.ReplaceAllString(cell, ""))
}

// table writes the header and rows, each column padded to its widest cell. The
// last column is never padded: trailing whitespace is invisible, survives a copy
// and paste, and shows up in a diff.
func (p *Printer) table(t Rows) error {
	// A lone header row over an empty result is noise a script has to filter out.
	// stdout stays genuinely empty; [Printer.Empty] says so on stderr instead.
	if len(t.Rows) == 0 {
		return nil
	}

	style := p.Style()

	lines := make([][]string, 0, len(t.Rows)+1)
	if len(t.Headers) > 0 {
		header := make([]string, len(t.Headers))
		for i, h := range t.Headers {
			header[i] = style.Bold(h)
		}
		lines = append(lines, header)
	}
	lines = append(lines, t.Rows...)

	widths := columnWidths(lines)

	var b strings.Builder
	for _, line := range lines {
		for i, cell := range line {
			if i == len(line)-1 {
				b.WriteString(cell)
				break
			}
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", widths[i]-width(cell)+gap))
		}
		b.WriteByte('\n')
	}

	if _, err := fmt.Fprint(p.out, b.String()); err != nil {
		return fmt.Errorf("render the table: %w", err)
	}
	return nil
}

// columnWidths measures each column across every line. A ragged row — one with
// fewer cells than the header — widens only the columns it actually has, rather
// than panicking on the ones it does not.
func columnWidths(lines [][]string) []int {
	var widths []int

	for _, line := range lines {
		for i, cell := range line {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], width(cell))
		}
	}
	return widths
}
