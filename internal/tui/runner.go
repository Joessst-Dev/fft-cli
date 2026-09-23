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

	// Background marks a run the UI started on its own — reading the user's roles,
	// signing in ahead of the first request — rather than one the user asked for.
	// It is left out of the request history, which is a record of what the user
	// sent, and whose counts the Operations list shows.
	Background bool

	// Stream asks for the run's output as it is produced, on [RunEvent.Chunk],
	// rather than only in the [Result] at the end. A run that does not end on its
	// own — the emulator, which serves until it is stopped — shows nothing at all
	// without it.
	//
	// It is opt-in because every chunk is an event, and the runner's event buffer
	// backs up onto the runs rather than onto the UI: streaming every run would
	// make a `--all` over a large tenant pay for output nobody is watching.
	Stream bool
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

	// Recorded says the run was added to the request history, and so that what
	// the UI read of the history is out of date.
	Recorded bool
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

	// Question, on a [RunRunning] event, is a question the command asks before it
	// goes on. The run waits until [Runner.Answer] answers it, or until it is
	// cancelled, which answers no.
	Question *Question

	// Chunk, on a [RunRunning] event, is output the run has produced since the
	// last one. It arrives only for an [Invocation] that asked to Stream, and it
	// says nothing about the run's state: a chunk is not a start, and a consumer
	// that times a run must leave its clock alone.
	Chunk *Chunk
}

// Chunk is part of a streaming run's output, as it was written.
//
// It is a copy the runner is done with, so a consumer may keep it. What arrives
// here is also still counted towards the capped [Result] the run ends with, so a
// screen that shows chunks live and a screen that shows the result at the end
// agree about what was written.
type Chunk struct {
	// Stderr says the bytes were written to standard error rather than standard
	// output. Each stream is coalesced on its own, and their chunks interleave in
	// the order the runner flushed them, which is not necessarily the order the
	// command wrote them in.
	Stderr bool

	// Bytes is the output, which is not split on any boundary: a chunk may end
	// mid-line, and mid-rune.
	Bytes []byte

	// Dropped says output was discarded before this chunk because the UI was not
	// keeping up. A run is never held up to deliver its output.
	Dropped bool
}

// Question is what a running command asks the user, in its own words: the
// confirmation `fft facility delete` asks in a shell, naming the facility it has
// looked up.
type Question struct {
	// ID tells this question from every other one the runner has asked. An answer
	// names it, so that it can only ever answer the question it was given for.
	ID uint64

	// Text is the command's question. It may quote what the API returned, and is
	// sanitized before it is drawn.
	Text string

	// Confirm, when set, is the word the user must type back to say yes. A command
	// that cannot be undone asks for one: a single key is too easy to press by
	// accident. "" means a y answers.
	Confirm string
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

	// Answer answers question q of run id. An answer to a question the run is no
	// longer asking — it was cancelled, or it has ended — does nothing.
	Answer(id RunID, q uint64, yes bool)

	// SetProject selects the project the invocations started after it act on. ""
	// leaves the choice to fft's own resolution: the active project, or the
	// environment.
	SetProject(name string)
}
