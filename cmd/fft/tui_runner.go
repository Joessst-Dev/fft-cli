package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// runnerSlots is how many commands the TUI runs at once. Enough that a slow
// request does not hold up the next one; few enough that a user holding down a
// key cannot open a flood of connections to their tenant.
const runnerSlots = 4

// runnerEventBuffer absorbs a burst of state changes while the UI is busy
// drawing. A full buffer blocks the runs, not the UI, which is the right way round.
const runnerEventBuffer = 64

// errRunnerClosed is what Start answers once the runner has been shut down.
var errRunnerClosed = errors.New("the command runner has been shut down")

// configWriters are the commands that rewrite the config file. Each runs alone,
// whatever the caller said: a run reading the file while another replaces it would
// see a file that is neither.
var configWriters = map[string]bool{
	"fft project add":       true,
	"fft project use":       true,
	"fft project remove":    true,
	"fft project read-only": true,
	"fft template save":     true,
	"fft template remove":   true,
}

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

	project atomic.Pointer[string]
	nextID  atomic.Uint64

	// mu guards closed and cancels, and makes Start's wg.Add and Close's wg.Wait
	// mutually exclusive — an Add racing a Wait is the one misuse WaitGroup cannot
	// survive.
	mu      sync.Mutex
	closed  bool
	cancels map[tui.RunID]context.CancelFunc
	wg      sync.WaitGroup
}

var _ tui.Runner = (*cliRunner)(nil)

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
	}
}

// Start implements [tui.Runner].
func (r *cliRunner) Start(inv tui.Invocation) (tui.RunID, error) {
	// The caller's slices are copied: the run outlives this call, and a UI that
	// reuses its buffers must not be able to rewrite a command already queued.
	inv.Args = slices.Clone(inv.Args)
	inv.Stdin = bytes.Clone(inv.Stdin)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, errRunnerClosed
	}

	id := tui.RunID(r.nextID.Add(1))
	ctx, cancel := context.WithCancel(r.ctx)
	r.cancels[id] = cancel

	r.wg.Add(1)
	go r.run(ctx, id, inv)
	return id, nil
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

// SetProject implements [tui.Runner].
func (r *cliRunner) SetProject(name string) { r.project.Store(&name) }

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

func (r *cliRunner) run(ctx context.Context, id tui.RunID, inv tui.Invocation) {
	defer r.wg.Done()
	defer r.forget(id)

	r.emit(tui.RunEvent{ID: id, State: tui.RunQueued, Invocation: inv, At: time.Now()})

	select {
	case r.slots <- struct{}{}:
	case <-ctx.Done():
		// Never started, so nothing was sent: it ends exactly as a command
		// interrupted before its first request would.
		r.finish(id, inv, tui.Result{ExitCode: exitcode.Interrupted})
		return
	}
	defer func() { <-r.slots }()

	// A cancel and a free slot can arrive together, and select chose the slot.
	if ctx.Err() != nil {
		r.finish(id, inv, tui.Result{ExitCode: exitcode.Interrupted})
		return
	}

	r.emit(tui.RunEvent{ID: id, State: tui.RunRunning, Invocation: inv, At: time.Now()})
	r.finish(id, inv, r.execute(ctx, inv))
}

func (r *cliRunner) execute(ctx context.Context, inv tui.Invocation) tui.Result {
	in := bytes.NewReader(inv.Stdin)
	deps := r.deps.forRun(in, r.session)

	var status atomic.Int64
	deps.observeStatus = func(code int) { status.Store(int64(code)) }

	var stdout, stderr bytes.Buffer
	root := newRootCmd(deps)

	target, _, findErr := root.Find(inv.Args)
	if findErr == nil {
		// A component is another process, and it would inherit the real terminal —
		// the one the UI is drawing on.
		if name, ok := target.Annotations[annotationComponent]; ok {
			err := exitcode.UsageError{Err: fmt.Errorf(
				"%q runs the %s component, which needs a terminal of its own; run it from a shell",
				target.CommandPath(), name)}
			return tui.Result{ExitCode: report(&stderr, err), Stderr: stderr.Bytes()}
		}
	}

	if inv.Exclusive || (findErr == nil && configWriters[target.CommandPath()]) {
		r.config.Lock()
		defer r.config.Unlock()
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

	started := time.Now()
	code := executeRoot(ctx, root, r.argv(inv.Args), in, &stdout, &stderr)

	return tui.Result{
		ExitCode: code,
		Status:   int(status.Load()),
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		Duration: time.Since(started),
	}
}

// argv is args with the flags every run inside the UI needs: the API's own JSON,
// which the UI parses, and the project the user selected unless args names one.
//
// They go before a "--", after which cobra would take them for positional
// arguments.
func (r *cliRunner) argv(args []string) []string {
	head, tail := args, []string(nil)
	if i := slices.Index(args, "--"); i >= 0 {
		head, tail = args[:i], args[i:]
	}

	out := slices.Concat(head, []string{"-o", "json", "--no-color"})
	if p := r.project.Load(); p != nil && *p != "" && !namesFlag(head, "project") {
		out = append(out, "--project", *p)
	}
	return append(out, tail...)
}

// namesFlag reports whether args sets the long flag name, in either spelling.
func namesFlag(args []string, name string) bool {
	return slices.ContainsFunc(args, func(arg string) bool {
		return arg == "--"+name || strings.HasPrefix(arg, "--"+name+"=")
	})
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
