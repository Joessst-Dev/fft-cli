package output

import "github.com/fatih/color"

// Style paints text for a terminal.
//
// A disabled Style returns its argument unchanged, so one call site produces a
// coloured table in a terminal and a byte-for-byte plain one under test — which
// is what makes a golden-file spec possible at all.
//
// Every colour is built with its own *color.Color and forced on explicitly.
// fatih/color otherwise consults a package-level NoColor global that it
// initialises from os.Stdout: a value that is wrong for a Printer writing
// somewhere else, and a data race between two specs running in parallel.
type Style struct{ enabled bool }

// Bold is for table headers.
func (s Style) Bold(v string) string { return s.paint(v, color.Bold) }

// Faint is for values a reader should skim past — an absent field, a hint.
func (s Style) Faint(v string) string { return s.paint(v, color.Faint) }

// Green, Yellow and Red carry meaning, not decoration: healthy, degraded,
// stopped. Choosing which is the caller's job, because what counts as healthy is
// the domain's business and not this package's.
func (s Style) Green(v string) string  { return s.paint(v, color.FgGreen) }
func (s Style) Yellow(v string) string { return s.paint(v, color.FgYellow) }
func (s Style) Red(v string) string    { return s.paint(v, color.FgRed) }

func (s Style) paint(v string, attrs ...color.Attribute) string {
	if !s.enabled || v == "" {
		return v
	}

	c := color.New(attrs...)
	c.EnableColor()
	return c.Sprint(v)
}
