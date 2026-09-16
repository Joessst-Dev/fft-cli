package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

const tuiLong = `Open fft's interactive mode: a full-screen UI in the terminal.

Every request the UI sends is an fft command line, run through the same command
tree as in a shell: the read-only gate, the exit codes and the validation all
apply unchanged.

The UI is drawn on stderr and needs a terminal on both stdin and stderr. It
writes nothing to stdout.`

// newTUICmd builds `fft tui`.
func newTUICmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Browse and send requests in an interactive terminal UI",
		Long:  tuiLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Refused up front rather than left to the renderer: in a pipe or a CI job
			// a full-screen UI is not degraded, it is garbage in a log that nobody can
			// answer.
			if !deps.interactiveTerminal(cmd) {
				return exitcode.UsageError{Err: errors.New(
					"fft tui needs a terminal on stdin and stderr; in a script, run the fft command itself")}
			}

			runner := newCLIRunner(cmd.Context(), deps)
			defer runner.Close()
			runner.SetProject(deps.Project)

			start := deps.StartTUI
			if start == nil {
				start = tui.Run
			}
			return start(cmd.Context(), tui.Options{
				Runner: runner,
				In:     cmd.InOrStdin(),
				Out:    cmd.ErrOrStderr(),
			})
		},
	}
}

// interactiveTerminal reports whether someone is at a terminal to drive the UI:
// keystrokes arrive on stdin, and the UI is drawn on stderr. [Deps.Terminal]
// overrides the answer, as it does for the update notice.
func (d *Deps) interactiveTerminal(cmd *cobra.Command) bool {
	if d.Terminal != nil {
		return *d.Terminal
	}
	return prompt.IsTerminal(cmd.InOrStdin()) && prompt.IsTerminal(cmd.ErrOrStderr())
}
