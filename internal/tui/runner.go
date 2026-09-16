// Package tui is fft's interactive terminal UI.
//
// It does not send requests itself. Every action it offers becomes an fft command
// line, and a [Runner] executes that line through the same command tree the shell
// reaches — so the read-only gate, the exit codes, curated validation and the
// output contract hold in the UI exactly as they do in a script, and every screen
// can show the command it stands for. The package defines the contract and
// cmd/fft implements it, which keeps this package free of an import of main.
package tui

import "time"

// RunID identifies one invocation for the lifetime of a [Runner].
type RunID uint64

// Invocation is one fft command line to execute.
type Invocation struct {
	// Args is the command line without the program name: {"facility", "list"}.
	// The runner adds the flags the UI depends on (machine-readable output, the
	// selected project), so Args carries only what the user chose.
	Args []string

	// Stdin is what the command reads from standard input. A request body and a
	// password travel here, never in Args, so neither is ever shown as part of a
	// command line or kept in a process listing.
	Stdin []byte

	// Exclusive runs the invocation alone, with no other run in flight. A command
	// that rewrites the config file needs it: a concurrent run would read the file
	// half-way through its rewrite.
	Exclusive bool
}

// RunState is where an invocation is in its life.
type RunState int

const (
	// RunQueued is an accepted invocation waiting for a free slot.
	RunQueued RunState = iota
	// RunRunning is an invocation that is executing.
	RunRunning
	// RunDone is an invocation that has finished, however it finished.
	RunDone
)

// Result is what a finished invocation produced.
type Result struct {
	// ExitCode is the exit code the same command line would have given the shell.
	ExitCode int

	// Status is the HTTP status of the last response the tenant sent, and 0 when
	// no response arrived — a refused write, a usage error, a cancelled run.
	Status int

	// Stdout is the command's data: the API's own document under -o json.
	Stdout []byte

	// Stderr is everything else the command said: errors, warnings, notices.
	Stderr []byte

	// Duration is how long the command executed, excluding the time it queued.
	Duration time.Duration
}

// RunEvent reports a change in an invocation's state.
type RunEvent struct {
	ID         RunID
	State      RunState
	Invocation Invocation

	// At is when the invocation entered State.
	At time.Time

	// Result is set when State is [RunDone], and zero otherwise.
	Result Result
}

// Runner executes invocations in the background and reports on them.
//
// Its lifecycle belongs to whoever built it, so there is no Close here: the UI
// only starts, cancels and watches.
type Runner interface {
	// Start accepts inv and returns at once. Its progress arrives on Events.
	Start(inv Invocation) (RunID, error)

	// Cancel stops a queued or running invocation, which then finishes with exit
	// code 130 as an interrupted command would. Cancelling a finished or unknown
	// run does nothing.
	Cancel(id RunID)

	// Events delivers every state change of every run. The runner closes it once
	// it has been shut down and its last run has finished.
	Events() <-chan RunEvent

	// SetProject selects the project later invocations act on. "" leaves the
	// choice to fft's own resolution: the active project, or the environment.
	SetProject(name string)
}
