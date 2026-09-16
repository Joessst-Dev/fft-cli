package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// runnerSlots is how many commands the TUI runs at once. Enough that a slow
// request does not hold up the next one; few enough that a user holding down a
// key cannot open a flood of connections to their tenant.
const runnerSlots = 4

// The most of a run's output the runner keeps. A response is held in memory for
// as long as the UI may show it, and `--all` over a large tenant can print tens of
// megabytes that nobody will scroll through on a terminal; the rest is counted and
// dropped, and the result says so.
const (
	runnerStdoutLimit = 8 << 20
	runnerStderrLimit = 256 << 10
)

// runnerEventBuffer absorbs a burst of state changes while the UI is busy
// drawing. A full buffer blocks the runs, not the UI, which is the right way round.
const runnerEventBuffer = 64

// errRunnerClosed is what Start answers once the runner has been shut down.
var errRunnerClosed = errors.New("the command runner has been shut down")

// annotationExclusive marks a command the TUI's runner must run alone, whatever the
// caller asked for. Its value names the files the command rewrites.
//
// Such a command reads a file, decides, and writes the file back. Every write is
// atomic, so a concurrent run never sees a torn file; the danger is a lost update.
// Two of them side by side each save what they read, and the second save silently
// undoes the first one's change — a project switched and then switched back by a
// `project read-only` that loaded the file before the switch. The guard spec in
// exclusive_test.go finds every command that saves the config file and fails
// until it carries this.
const annotationExclusive = "exclusive"

const (
	// exclusiveConfig is the config file.
	exclusiveConfig = "config"

	// exclusiveTemplates is the template directory. Saving checks that the name is
	// free before it writes, and removing checks that the file is there: two saves
	// of one name side by side would both find it free, and the second would
	// overwrite the first without the --force it would otherwise have required.
	exclusiveTemplates = "templates"

	// exclusiveHistory is the request history. Clearing it reads the file for the
	// count it reports and then deletes it; a run finishing in between would be
	// recorded and deleted uncounted, or recorded into the history after the clear
	// although it ran before it.
	exclusiveHistory = "history"
)

// cliRunner is the TUI's [tui.Runner]: it executes each invocation through
// [executeRoot], on a Deps of its own, in the background.
type cliRunner struct {
	deps    *Deps
	session uiRun
	ctx     context.Context
	stop    context.CancelFunc
	events  chan tui.RunEvent
	slots   chan struct{}

	// config is held for reading by every run and for writing by an exclusive one.
	config sync.RWMutex

	nextID atomic.Uint64

	// mu guards everything below, and makes Start's wg.Add and Close's wg.Wait
	// mutually exclusive — an Add racing a Wait is the one misuse WaitGroup cannot
	// survive.
	mu      sync.Mutex
	closed  bool
	cancels map[tui.RunID]context.CancelFunc
	wg      sync.WaitGroup

	// project is the project the UI has selected. A run takes it when it is
	// started, not when it executes: the user confirmed it against the project
	// on screen then, and a switch while it queues must not redirect it.
	project string

	// catalog is a command tree that is never executed. Start resolves a command
	// line in it to learn whether the command must run alone, which has to be
	// known before the run is queued so that its place in line can be kept.
	// cobra builds flag sets lazily as it looks commands up, so it is only used
	// under mu.
	catalog *cobra.Command

	// lastExclusive is closed once the most recently started exclusive run has
	// finished, nil when there has been none.
	lastExclusive chan struct{}

	// tokens is the session's token sources, shared by every run.
	tokens sessionTokens

	// stdoutLimit and stderrLimit are how much of each run's output is kept.
	stdoutLimit, stderrLimit int
}

var _ tui.Runner = (*cliRunner)(nil)

// job is one accepted invocation, with what was decided about it when it was
// started.
type job struct {
	inv tui.Invocation

	// stdin is inv.Stdin, taken out of inv so that no event carries it. The
	// runner owns this copy and clears it once the run is over.
	stdin []byte

	// ui is the session as it was at Start, the selected project included.
	ui uiRun

	// exclusive runs the job alone. after, when set, is closed once the
	// exclusive job started before it has finished, and done is closed when this
	// one has.
	exclusive bool
	after     <-chan struct{}
	done      chan struct{}
}

// newCLIRunner returns a runner whose runs are all cancelled when ctx is. deps is
// the template each run's own Deps is cut from, and session the flags `fft tui` was
// started with; see [Deps.forRun].
func newCLIRunner(ctx context.Context, deps *Deps, session uiRun) *cliRunner {
	ctx, stop := context.WithCancel(ctx)
	return &cliRunner{
		deps:    deps,
		session: session,
		ctx:     ctx,
		stop:    stop,
		events:  make(chan tui.RunEvent, runnerEventBuffer),
		slots:   make(chan struct{}, runnerSlots),
		cancels: make(map[tui.RunID]context.CancelFunc),
		catalog: newRootCmd(deps.forRun(nil, session)),

		stdoutLimit: runnerStdoutLimit,
		stderrLimit: runnerStderrLimit,
	}
}

// Start implements [tui.Runner].
//
// Everything a run depends on that can change while it waits is decided here:
// the project it acts on, and its place among the runs that must run alone. The
// session's read-only floor and timeout are fixed for the runner's life.
func (r *cliRunner) Start(inv tui.Invocation) (tui.RunID, error) {
	// The caller's slices are copied: the run outlives this call, and a UI that
	// reuses its buffers must not be able to rewrite a command already queued.
	j := job{stdin: bytes.Clone(inv.Stdin)}
	inv.Args = slices.Clone(inv.Args)
	inv.Stdin = nil

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, errRunnerClosed
	}

	j.ui = r.session
	j.ui.project = r.project
	if inv.Project != "" {
		j.ui.project = inv.Project
	}

	j.exclusive = inv.Exclusive || r.rewritesSharedFile(inv.Args)
	inv.Exclusive = j.exclusive
	j.inv = inv
	if j.exclusive {
		// In the order they were started: two switches in a row must end on the
		// second, and a sign-in queued after a switch must follow it.
		j.after = r.lastExclusive
		j.done = make(chan struct{})
		r.lastExclusive = j.done
	}

	id := tui.RunID(r.nextID.Add(1))
	ctx, cancel := context.WithCancel(r.ctx)
	r.cancels[id] = cancel

	r.wg.Add(1)
	go r.run(ctx, id, j)
	return id, nil
}

// rewritesSharedFile reports whether args resolve to a command marked
// [annotationExclusive]. It must be called with mu held.
func (r *cliRunner) rewritesSharedFile(args []string) bool {
	target, _, err := r.catalog.Find(args)
	return err == nil && target.Annotations[annotationExclusive] != ""
}

// Cancel implements [tui.Runner].
func (r *cliRunner) Cancel(id tui.RunID) {
	r.mu.Lock()
	cancel := r.cancels[id]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Events implements [tui.Runner].
func (r *cliRunner) Events() <-chan tui.RunEvent { return r.events }

// SetProject implements [tui.Runner]. It applies to the runs started after it.
func (r *cliRunner) SetProject(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.project = name
	r.tokens.forget()
}

// Close cancels every run, waits for them to finish, and closes Events. It is
// safe to call more than once.
func (r *cliRunner) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()

	r.stop()
	// Outside mu: a finishing run takes it to forget its cancel func.
	r.wg.Wait()
	close(r.events)
}

func (r *cliRunner) run(ctx context.Context, id tui.RunID, j job) {
	defer r.wg.Done()
	defer r.forget(id)
	if j.done != nil {
		// Deferred after the slot and the config file are released, and after the
		// last event: the next exclusive run starts only once this one has ended.
		defer close(j.done)
	}
	// A secret on stdin is not kept in memory any longer than the run needs it.
	defer clear(j.stdin)

	r.emit(tui.RunEvent{ID: id, State: tui.RunQueued, Invocation: j.inv, At: time.Now()})

	if j.after != nil {
		select {
		case <-j.after:
		case <-ctx.Done():
			r.finish(id, j.inv, tui.Result{ExitCode: exitcode.Interrupted})
			return
		}
	}

	select {
	case r.slots <- struct{}{}:
	case <-ctx.Done():
		// Never started, so nothing was sent: it ends exactly as a command
		// interrupted before its first request would.
		r.finish(id, j.inv, tui.Result{ExitCode: exitcode.Interrupted})
		return
	}
	defer func() { <-r.slots }()

	// A cancel and a free slot can arrive together, and select chose the slot.
	if ctx.Err() != nil {
		r.finish(id, j.inv, tui.Result{ExitCode: exitcode.Interrupted})
		return
	}

	r.emit(tui.RunEvent{ID: id, State: tui.RunRunning, Invocation: j.inv, At: time.Now()})
	r.finish(id, j.inv, r.execute(ctx, j))
}

// execute runs one job on a Deps of its own, signing its requests through the
// session's token sources.
func (r *cliRunner) execute(ctx context.Context, j job) tui.Result {
	in := bytes.NewReader(j.stdin)
	deps := r.deps.forRun(in, j.ui)
	deps.tokens = &r.tokens

	var status atomic.Int64
	deps.observeStatus = func(code int) { status.Store(int64(code)) }

	if j.exclusive {
		r.config.Lock()
		defer r.config.Unlock()
		// A command that runs alone may have rewritten the config file — replaced a
		// project's account, removed it — and a token kept from before would sign the
		// next run in as whoever the project used to be. Forgotten before the lock is
		// released, so that no run can pick the old one up in between.
		defer r.tokens.forget()
	} else {
		r.config.RLock()
		defer r.config.RUnlock()
	}

	// Waiting for the lock can outlast a cancel. A command whose turn came after it
	// was cancelled has not started, and must not: `project use` does not look at
	// its context, and would switch the project the user just said not to.
	if ctx.Err() != nil {
		return tui.Result{ExitCode: exitcode.Interrupted}
	}

	stdout := cappedBuffer{limit: r.stdoutLimit}
	stderr := cappedBuffer{limit: r.stderrLimit}
	started := time.Now()
	// The command line is run exactly as given; the run's Deps carries what the UI
	// decides.
	code := executeRoot(ctx, deps, newRootCmd(deps), j.inv.Args, in, &stdout, &stderr)

	res := tui.Result{
		ExitCode:        code,
		Status:          int(status.Load()),
		Stdout:          stdout.buf.Bytes(),
		Stderr:          stderr.buf.Bytes(),
		StdoutTruncated: stdout.dropped,
		StderrTruncated: stderr.dropped,
		Duration:        time.Since(started),
	}
	if p := deps.run.project.Load(); p != nil {
		res.Project = *p
	}
	return res
}

// cappedBuffer keeps the first limit bytes written to it, and drops the rest.
type cappedBuffer struct {
	buf     bytes.Buffer
	limit   int
	dropped bool
}

// Write never fails, and always reports everything as written: a command whose
// output the UI has stopped keeping must still finish, and report how it ended.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	room := max(b.limit-b.buf.Len(), 0)
	kept := p[:min(room, len(p))]
	b.buf.Write(kept)
	if len(kept) < len(p) {
		b.dropped = true
	}
	return len(p), nil
}

func (r *cliRunner) finish(id tui.RunID, inv tui.Invocation, res tui.Result) {
	r.emit(tui.RunEvent{ID: id, State: tui.RunDone, Invocation: inv, At: time.Now(), Result: res})
}

// emit delivers ev, or drops it once the runner is shutting down and nobody is
// left to read it. Blocking there would keep Close waiting forever.
func (r *cliRunner) emit(ev tui.RunEvent) {
	// Room in the buffer wins outright. A select over both cases would pick at
	// random once shutdown has begun, dropping events there was space for.
	select {
	case r.events <- ev:
		return
	default:
	}

	select {
	case r.events <- ev:
	case <-r.ctx.Done():
	}
}

func (r *cliRunner) forget(id tui.RunID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cancel, ok := r.cancels[id]; ok {
		cancel()
		delete(r.cancels, id)
	}
}
