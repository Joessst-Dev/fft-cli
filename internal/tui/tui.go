package tui

import (
	"context"
	"errors"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Options is what [Run] needs from its caller.
type Options struct {
	// Runner executes the commands the UI builds.
	Runner Runner

	// In is where keystrokes come from; it must be a terminal.
	In io.Reader

	// Out is where the UI is drawn. fft passes stderr, so that stdout stays what
	// it is everywhere else: data, and here nothing at all.
	Out io.Writer

	// Project is the project the session starts on, "" to leave the choice to
	// fft's own resolution. It is only shown; the Runner already acts on it.
	Project string

	// ReadOnly is set when every write in the session is refused, whatever the
	// project's configuration says: fft tui --read-only, or FFT_READ_ONLY.
	ReadOnly bool

	// Color draws the UI in colour. Without it nothing depends on colour to be
	// understood.
	Color bool

	// Now is the clock the UI measures elapsed time and token lifetimes with. nil
	// means time.Now.
	Now func() time.Time
}

// Run shows the UI until the user quits or ctx is cancelled. Runs still in flight
// when it returns are the Runner owner's to cancel.
func Run(ctx context.Context, opts Options) error {
	if opts.Runner == nil {
		return errors.New("tui: no runner to execute commands with")
	}
	p := tea.NewProgram(newApp(opts),
		tea.WithContext(ctx),
		tea.WithInput(opts.In),
		tea.WithOutput(opts.Out),
	)
	_, err := p.Run()
	return err
}
