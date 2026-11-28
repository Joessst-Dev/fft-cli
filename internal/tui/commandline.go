package tui

import (
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/shellquote"
)

// unportableMarker follows a command that cannot be pasted into a shell as shown.
const unportableMarker = "  (cannot be copied safely)"

// shellCommand is the fft command a user would type in a shell to do what the UI
// is about to do. It is what the status bar and the dialogs show, and what y
// copies.
type shellCommand struct {
	line string

	// unportable is set when an argument holds a character that no one quoting
	// keeps intact in every shell the line may be pasted into. Such a line is
	// shown for what it says, marked, and never copied: a paste that a shell
	// reads differently runs a command nobody saw.
	unportable bool
}

// String is the command as the UI shows it: the line, and the marker when it
// cannot be copied.
func (c shellCommand) String() string {
	if c.unportable {
		return c.line + unportableMarker
	}
	return c.line
}

func (c shellCommand) empty() bool { return c.line == "" }

// nounArgs is `fft <noun> <verb> <name> <flags>`. A name starting with a dash
// goes last, after "--", where it is only a name and never a flag.
//
// fft refuses to create a project or template whose name starts with a dash, but
// the names reach the UI from places that rule does not guard: the config file
// (an older fft, or a hand edit), the template directory listing, a component
// manifest, or what the user typed as a component source.
func nounArgs(noun, verb, name string, flags ...string) []string {
	args := []string{noun, verb}
	if strings.HasPrefix(name, "-") {
		return append(append(append(args, flags...), "--"), name)
	}
	return append(append(args, name), flags...)
}

// commandLine renders args as the fft command a user would type.
func commandLine(args []string) shellCommand {
	var b strings.Builder
	b.WriteString("fft")
	cmd := shellCommand{}
	for _, a := range args {
		quoted, portable := shellQuote(a)
		b.WriteByte(' ')
		b.WriteString(quoted)
		cmd.unportable = cmd.unportable || !portable
	}
	cmd.line = b.String()
	return cmd
}

// ShellQuote is s spelled for a command line that fft prints for a user to paste:
// [shellquote.Quote]'s spelling, without the verdict on whether it is portable.
func ShellQuote(s string) string {
	quoted, _ := shellQuote(s)
	return quoted
}

// shellQuote is [shellquote.Quote]; the quoting lives in its own package so the
// network report, which has no business importing a terminal UI, shares it.
func shellQuote(s string) (quoted string, portable bool) { return shellquote.Quote(s) }
