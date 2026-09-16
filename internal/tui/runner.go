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
	// It is run exactly as given; what the UI decides for every run — the output
	// format, the selected project — travels beside it, so Args carries only what
	// the user chose.
	//
	// Args is not secret. It is shown as the equivalent fft command and kept, with
	// known credential-shaped values redacted, in the request history; redaction is
	// a net, not a place to put a secret. Anything sensitive belongs on Stdin.
	Args []string

	// Stdin is what the command reads from standard input. A request body and a
	// password travel here, never in Args, so neither is ever shown as part of a
	// command line or kept in a process listing.
	Stdin []byte

	// Project, when set, is the project the invocation acts on, whichever one the
	// UI has selected since. It is how a request is sent again to the project it
	// was first sent to. It travels beside Args, like the selection it overrides.
	Project string

	// Exclusive runs the invocation alone, with no other run in flight, and after
	// every exclusive invocation started before it. The runner already runs alone
	// every command that reads a shared file and writes it back, since two of those
	// side by side lose one of their updates, and reports those as exclusive in its
	// events; this is for a sequence the UI knows must not interleave with anything
	// else.
	Exclusive bool
}

// RunState is where an invocation is in its life.
type RunState int

const (
	// RunQueued is an accepted invocation waiting for a free slot.
	RunQueued RunState = iota
	// RunRunning is an invocation that holds a slot: it is executing, or waiting
	// for a command that needs the config file to itself to finish.
	RunRunning
	// RunDone is an invocation that has finished, however it finished.
	RunDone
)

// Result is what a finished invocation produced.
type Result struct {
	// ExitCode is the exit code the same command line would have given the shell.
	ExitCode int

	// Status is the HTTP status of the last response the tenant sent, and 0 when
	// no response arrived — a refused write, a usage error, a cancelled run. A
	// command that sends requests side by side reports the one that finished last,
	// which is not necessarily the one it sent last.
	Status int

	// Stdout is the command's data: the API's own document under -o json.
	Stdout []byte

	// Stderr is everything else the command said: errors, warnings, notices.
	Stderr []byte

	// StdoutTruncated and StderrTruncated say the command wrote more than the
	// runner keeps, and the stream holds only the first part of what it wrote. A
	// truncated document is not the API's answer, and must not pass for one.
	StdoutTruncated bool
	StderrTruncated bool

	// Project is the project the command acted on, "" when it never got as far as
	// choosing one.
	Project string

	// Duration is how long the command executed, excluding the time it queued.
	Duration time.Duration
}

// RunEvent reports a change in an invocation's state.
type RunEvent struct {
	ID    RunID
	State RunState

	// Invocation is the invocation as the runner accepted it. Its Stdin is always
	// nil: what a run reads may be a secret, and an event is not where it goes.
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
	// Start accepts inv and returns at once. Its progress arrives on Events. The
	// project it acts on is the one selected when Start is called, however long
	// the run then waits for its turn.
	Start(inv Invocation) (RunID, error)

	// Cancel stops an invocation. One that has not started executing ends with exit
	// code 130 and has sent nothing. One that is executing ends as the command does
	// when interrupted: 130 if the interruption stopped it, and its own result if
	// it finished its work first — a write that landed is reported as landed.
	// Cancelling a finished or unknown run does nothing.
	Cancel(id RunID)

	// Events delivers every state change of every run. The runner closes it once
	// it has been shut down and its last run has finished.
	Events() <-chan RunEvent

	// SetProject selects the project the invocations started after it act on. ""
	// leaves the choice to fft's own resolution: the active project, or the
	// environment.
	SetProject(name string)
}
