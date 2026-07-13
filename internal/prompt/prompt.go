// Package prompt asks the user questions on an interactive terminal.
//
// Prompts are written to stderr, never stdout: a command that asks a question
// must still be safe to pipe. Everything here is driven through an io.Reader so
// that the specs can feed it a buffer instead of a terminal.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ErrNotInteractive is returned when a prompt is needed but there is no terminal
// to ask on — a CI job, or a command whose stdin is a pipe. Commands turn it
// into "pass --flag instead", which is the only useful thing to say.
var ErrNotInteractive = errors.New("no interactive terminal")

// Prompter reads answers from in and writes questions to out.
type Prompter struct {
	in  io.Reader
	out io.Writer

	// reader is buffered, and must be reused across calls: a bufio.Reader may
	// read past the newline it returns, so a fresh one per prompt would drop the
	// user's next answer.
	reader *bufio.Reader

	// interactive overrides the terminal detection. It is a seam, in the same
	// spirit as client.Retry.Sleep: a bytes.Buffer has no file descriptor, so
	// without it a suite could never exercise an interactive answer at all — only
	// the refusal — and the question a destructive command asks would go untested.
	// nil means "ask the file descriptor", which is what production does.
	interactive *bool
}

// Option configures a Prompter.
type Option func(*Prompter)

// WithInteractive overrides whether in is treated as a terminal.
//
// Nothing in fft passes it: production reads the answer off the file descriptor.
// The specs pass it, because that is the only way to drive the question a
// destructive command asks.
func WithInteractive(v bool) Option {
	return func(p *Prompter) { p.interactive = &v }
}

// New returns a Prompter reading from in and writing prompts to out. out should
// be stderr.
func New(in io.Reader, out io.Writer, opts ...Option) *Prompter {
	p := &Prompter{in: in, out: out, reader: bufio.NewReader(in)}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Interactive reports whether in is a terminal a user could answer on.
func (p *Prompter) Interactive() bool {
	if p.interactive != nil {
		return *p.interactive
	}
	return IsTerminal(p.in)
}

// IsTerminal reports whether v is an interactive terminal. It accepts anything
// carrying a file descriptor, so it answers for os.Stdin and os.Stdout alike;
// a bytes.Buffer has none, which is exactly why a spec's streams are never
// mistaken for a terminal.
func IsTerminal(v any) bool {
	f, ok := v.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Line asks for a value, offering def as the default. An empty answer takes the
// default.
func (p *Prompter) Line(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", label)
	}

	line, err := p.readLine()
	if err != nil {
		return "", err
	}
	if line == "" {
		return def, nil
	}
	return line, nil
}

// Required asks for a value and keeps asking until it gets a non-empty one.
func (p *Prompter) Required(label string) (string, error) {
	for {
		val, err := p.Line(label, "")
		if err != nil {
			return "", err
		}
		if val != "" {
			return val, nil
		}
		fmt.Fprintln(p.out, "A value is required.")
	}
}

// Password asks for a secret. On a terminal the input is masked; otherwise it is
// read as a plain line, which is what lets the specs drive it.
func (p *Prompter) Password(label string) (string, error) {
	fmt.Fprintf(p.out, "%s: ", label)

	f, ok := p.in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return p.readLine()
	}

	secret, err := term.ReadPassword(int(f.Fd()))
	// The terminal swallowed the user's Enter, so the cursor is still on the
	// prompt line. Put it back where the next write expects it.
	fmt.Fprintln(p.out)
	if err != nil {
		return "", fmt.Errorf("read the password: %w", err)
	}
	return string(secret), nil
}

// Confirm asks a yes/no question. Anything other than "y" or "yes" is a no —
// destructive commands should never proceed on an ambiguous answer.
func (p *Prompter) Confirm(label string) (bool, error) {
	fmt.Fprintf(p.out, "%s [y/N]: ", label)

	line, err := p.readLine()
	if err != nil {
		return false, err
	}

	switch strings.ToLower(line) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// readLine reads one answer. EOF on the first byte means the user closed stdin
// (or there never was one), which is an empty answer rather than a failure.
func (p *Prompter) readLine() (string, error) {
	line, err := p.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// ReadAll reads a secret piped on stdin, for --password-stdin. It trims a single
// trailing newline — `echo secret | fft ...` appends one and the user did not
// mean it to be part of the password — but nothing else, because leading and
// trailing spaces can be genuine.
func ReadAll(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}

	s := strings.TrimSuffix(string(data), "\n")
	s = strings.TrimSuffix(s, "\r")
	return s, nil
}
