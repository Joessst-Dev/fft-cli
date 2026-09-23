package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
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

// How a streaming run's output is forwarded to the UI.
//
// The flush interval coalesces: the emulator with --verbose logs a line per
// request, and one event per line would spend the whole event buffer on a screen
// that redraws far less often than that. The chunk limit bounds what one flush
// carries — beyond it the *newest* bytes are kept, because what a live log shows
// is its tail, and the run's own capped output still holds the whole of what was
// written.
const (
	runnerStreamFlush = 100 * time.Millisecond
	runnerStreamChunk = 64 << 10
)

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

// annotationConfirms marks a command that asks before it acts unless --yes is
// given; confirms_test.go finds every command that asks and fails until it
// carries this.
//
// The TUI never passes --yes to such a command. The command asks its own question
// — the one that names the facility it looked up, or the number of listings it is
// about to purge — and the runner hands that question to the UI to answer. A --yes
// would skip it, and leave the user confirming a summary of the operation instead.
//
// The value is [confirmsYes] when a y answers. A command that cannot be undone
// names instead the word the UI makes the user type back: its own verb, which its
// question states and no stray keystroke produces. The id the command resolved
// would be no better — a UUID is pasted, not typed — and the UI only has it as
// part of the question's prose.
const annotationConfirms = "confirms"

// confirmsYes is the [annotationConfirms] value of a question a y answers.
const confirmsYes = "true"

// annotationManagesProjects marks a command that manages fft's own configuration
// rather than acting on a tenant: the `fft project` group. It is set on the group
// and found by walking up from the command, since cobra does not inherit
// annotations.
//
// Such a command always runs against the *real* environment, whatever the session
// has been pointed at. A run with an environment of its own is headless, and a
// headless `fft project list` reports only the environment's project — so pointing
// the session at the emulator would empty the Projects screen of the very rows the
// user switches back from, and `project use` would refuse. The config file is the
// same file whichever tenant the session is talking to.
const annotationManagesProjects = "managesProjects"

// annotationRescans marks a command that changes what is installed under the
// component root. The TUI's runner re-reads the registry after one of them
// succeeds, so that the next run — and the command tree built for it — sees what
// just appeared or went away, rather than the scan taken when the UI started.
const annotationRescans = "rescansComponents"

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

	nextID       atomic.Uint64
	nextQuestion atomic.Uint64

	// mu guards everything below, and makes Start's wg.Add and Close's wg.Wait
	// mutually exclusive — an Add racing a Wait is the one misuse WaitGroup cannot
	// survive.
	mu      sync.Mutex
	closed  bool
	cancels map[tui.RunID]context.CancelFunc
	wg      sync.WaitGroup

	// questions are the questions runs are waiting on, one per run at most.
	questions map[tui.RunID]*pendingQuestion

	// project is the project the UI has selected. A run takes it when it is
	// started, not when it executes: the user confirmed it against the project
	// on screen then, and a switch while it queues must not redirect it.
	project string

	// emulator is the base URL of the local emulator the UI has pointed the session
	// at, "" when its runs go to a configured project. Taken at Start for the same
	// reason project is.
	emulator string

	// catalog is a command tree that is never executed. Start resolves a command
	// line in it to learn whether the command must run alone, which has to be
	// known before the run is queued so that its place in line can be kept.
	// cobra builds flag sets lazily as it looks commands up, so it is only used
	// under mu.
	catalog *cobra.Command

	// ops is what the UI lists and builds its forms from, read off catalog once.
	ops *cliCatalog

	// lastExclusive is closed once the most recently started exclusive run has
	// finished, nil when there has been none.
	lastExclusive chan struct{}

	// tokens is the session's token sources, shared by every run.
	tokens sessionTokens

	// components is the component registry each run is given, and the command tree
	// for it is built from. It is replaced after a run that installs, upgrades or
	// removes one: [component.Open] scans the root once, so the registry the UI
	// started with would otherwise go on describing a directory that has changed
	// underneath it.
	components atomic.Pointer[component.Registry]

	// rescanMu serializes rescanComponents against itself. Two of the commands it
	// follows can finish close together, and without a lock the rescan that started
	// first could still be the one that stores last — reverting the registry to a
	// state that predates the more recent install or removal.
	rescanMu sync.Mutex

	// stdoutLimit and stderrLimit are how much of each run's output is kept.
	stdoutLimit, stderrLimit int

	// streamFlush and streamChunk are how a streaming run's output is forwarded;
	// see [runnerStreamFlush].
	streamFlush time.Duration
	streamChunk int
}

var _ tui.Runner = (*cliRunner)(nil)

// job is one accepted invocation, with what was decided about it when it was
// started.
type job struct {
	inv tui.Invocation

	// stdin is inv.Stdin, taken out of inv so that no event carries it. The
	// runner owns this copy and clears it once the run is over.
	stdin []byte

	// ui is the session as it was at Start, the selected project included, and
	// whether the UI started this run on its own.
	ui uiRun

	// confirm is the word the user types to answer the command's question yes, ""
	// when a y answers it. See [annotationConfirms].
	confirm string

	// exclusive runs the job alone. after, when set, is closed once the
	// exclusive job started before it has finished, and done is closed when this
	// one has.
	exclusive bool
	after     <-chan struct{}
	done      chan struct{}

	// rewritesConfig is set for a command that saves the config file, whose tokens
	// are forgotten once it finishes. An exclusive run is not necessarily one: the
	// UI also runs a sign-in alone, and the token it mints is the point of it.
	rewritesConfig bool

	// rescansComponents is set for a command that changes what is installed under
	// the component root; see [annotationRescans].
	rescansComponents bool
}

// newCLIRunner returns a runner whose runs are all cancelled when ctx is. deps is
// the template each run's own Deps is cut from, and session the flags `fft tui` was
// started with; see [Deps.forRun].
func newCLIRunner(ctx context.Context, deps *Deps, session uiRun) *cliRunner {
	ctx, stop := context.WithCancel(ctx)
	// Resolved here rather than taken as it is, so that the registry the runs are
	// given is never nil and can always be replaced by one read later.
	deps.openComponents()
	tree := newRootCmd(deps.forRun(nil, session))
	r := &cliRunner{
		deps:    deps,
		session: session,
		ctx:     ctx,
		stop:    stop,
		events:  make(chan tui.RunEvent, runnerEventBuffer),
		slots:   make(chan struct{}, runnerSlots),
		cancels: make(map[tui.RunID]context.CancelFunc),
		catalog: tree,

		questions: make(map[tui.RunID]*pendingQuestion),
		// Before the runner is returned, and so before Start can walk the same tree
		// from another goroutine.
		ops: newCLICatalog(tree),

		stdoutLimit: runnerStdoutLimit,
		stderrLimit: runnerStderrLimit,
		streamFlush: runnerStreamFlush,
		streamChunk: runnerStreamChunk,
	}
	r.components.Store(deps.Components)
	return r
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
	j.ui.background = inv.Background
	j.ui.stream = inv.Stream

	cl := r.classify(inv.Args)
	if r.emulator != "" && !cl.managesProjects {
		// The emulator is reached the way a shell reaches it: through the environment,
		// which is also the only way — it cannot stand in for Google's sign-in. The
		// selected project goes with it, since that environment names its own.
		j.ui.env = config.EmulatorEnv(r.emulator)
		j.ui.project = ""
	}
	j.confirm = cl.confirm
	j.rewritesConfig = cl.rewrites == exclusiveConfig
	j.rescansComponents = cl.rescans
	j.exclusive = inv.Exclusive || cl.rewrites != ""
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

// classification is what the catalog says about a command line. It is decided
// when the run is queued, because its place in line depends on it.
type classification struct {
	// rewrites are the files the command rewrites, "" when nothing requires it to
	// run alone ([annotationExclusive]).
	rewrites string

	// confirm is what a user types to confirm its question ([annotationConfirms]),
	// "" when a y answers it or it asks nothing.
	confirm string

	// rescans says the command changes what is installed under the component root
	// ([annotationRescans]).
	rescans bool

	// managesProjects says the command acts on the config file rather than on a
	// tenant, so it runs against the real environment ([annotationManagesProjects]).
	managesProjects bool
}

// classify resolves args in the catalog tree and reports what it says about the
// command. It must be called with mu held.
func (r *cliRunner) classify(args []string) classification {
	target, _, err := r.catalog.Find(args)
	if err != nil {
		return classification{}
	}
	cl := classification{
		rewrites: target.Annotations[annotationExclusive],
		rescans:  target.Annotations[annotationRescans] != "",
	}
	for c := target; c != nil; c = c.Parent() {
		if c.Annotations[annotationManagesProjects] != "" {
			cl.managesProjects = true
			break
		}
	}
	if word := target.Annotations[annotationConfirms]; word != confirmsYes {
		cl.confirm = word
	}
	return cl
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

// Answer implements [tui.Runner].
func (r *cliRunner) Answer(id tui.RunID, q uint64, yes bool) {
	r.mu.Lock()
	p := r.questions[id]
	if p == nil || p.id != q {
		r.mu.Unlock()
		return
	}
	// Taken out under mu, so that exactly one answer is ever sent: the channel's
	// one slot is always free for it.
	delete(r.questions, id)
	r.mu.Unlock()
	p.answer <- yes
}

// pendingQuestion is a question a run is waiting on.
type pendingQuestion struct {
	id     uint64
	answer chan bool
}

// confirmer is run id's [prompt.Confirmer]: it hands the command's question to the
// UI, and waits for the answer or for the run to be cancelled. A cancel is never a
// yes, even when an answer arrives with it.
func (r *cliRunner) confirmer(ctx context.Context, id tui.RunID, j job) prompt.Confirmer {
	return func(text string) (bool, error) {
		p := &pendingQuestion{id: r.nextQuestion.Add(1), answer: make(chan bool, 1)}
		r.mu.Lock()
		r.questions[id] = p
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			if r.questions[id] == p {
				delete(r.questions, id)
			}
			r.mu.Unlock()
		}()

		r.emit(tui.RunEvent{
			ID: id, State: tui.RunRunning, Invocation: j.inv, At: time.Now(),
			Question: &tui.Question{ID: p.id, Text: text, Confirm: j.confirm},
		})
		select {
		case yes := <-p.answer:
			if err := ctx.Err(); err != nil {
				return false, err
			}
			return yes, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

// Catalog is every operation the runner can send, and the command that sends it.
func (r *cliRunner) Catalog() tui.Catalog { return r.ops }

// Events implements [tui.Runner].
func (r *cliRunner) Events() <-chan tui.RunEvent { return r.events }

// SetProject implements [tui.Runner]. It applies to the runs started after it.
func (r *cliRunner) SetProject(name string) {
	r.mu.Lock()
	r.project = name
	r.mu.Unlock()
	// Outside mu, which every Start takes: holding one lock while taking another is
	// an order every other path would have to keep.
	r.tokens.forget()
}

// sessionRun is what the session's decisions amount to right now: the selected
// project, or the emulator the UI has pointed it at. [cliRunner.Start] refines it
// per invocation; a caller that is not a run — the History screen, which asks why
// nothing is being recorded — reads it as it stands.
func (r *cliRunner) sessionRun() uiRun {
	r.mu.Lock()
	defer r.mu.Unlock()

	ui := r.session
	ui.project = r.project
	if r.emulator != "" {
		ui.env = config.EmulatorEnv(r.emulator)
		ui.project = ""
	}
	return ui
}

// SetEmulator implements [tui.Runner]. It applies to the runs started after it.
func (r *cliRunner) SetEmulator(baseURL string) {
	r.mu.Lock()
	r.emulator = baseURL
	r.mu.Unlock()
	// Outside mu, as SetProject does it. The tokens are keyed by the project they
	// were minted for, so nothing would be handed to the wrong tenant either way;
	// they are dropped because a switch is also a good moment to stop holding a real
	// tenant's credential in memory.
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
	r.finish(id, j.inv, r.execute(ctx, id, j))
}

// execute runs one job on a Deps of its own, signing its requests through the
// session's token sources.
func (r *cliRunner) execute(ctx context.Context, id tui.RunID, j job) tui.Result {
	in := bytes.NewReader(j.stdin)
	deps := r.deps.forRun(in, j.ui)
	deps.tokens = &r.tokens
	// The registry as it is now, not as it was when the UI started: a component
	// installed a moment ago is one this run's command tree should have in it.
	deps.Components = r.components.Load()

	var status atomic.Int64
	deps.observeStatus = func(code int) { status.Store(int64(code)) }

	switch {
	case j.exclusive:
		r.config.Lock()
		defer r.config.Unlock()
		if j.rewritesConfig {
			// The command may have replaced a project's account, or removed it, and a
			// token kept from before would sign the next run in as whoever the project
			// used to be. Forgotten before the lock is released, so that no run can
			// pick the old one up in between.
			defer r.tokens.forget()
		}

	case j.inv.Stream:
		// Deliberately unlocked. A streamed run is one that does not end on its own —
		// the emulator serves until it is stopped — and holding the config file's read
		// lock for that long would make the next `project use` wait for the server to
		// be stopped. Worse, Go's RWMutex queues new readers behind a waiting writer,
		// so every run after that one would wait too: one `fft emulator` started from
		// the UI would freeze it.
		//
		// What the lock protects against is a lost update between a command that reads
		// the config file, decides, and writes it back. A streamed run is a component
		// server: it is handed its session once, before the child starts, and never
		// reads the file again. A save that lands during that one read is atomic, so
		// the child gets the file as it was before or as it is after — and a switch the
		// user made after starting a server is not a switch that server was promised.
		//
		// The one read-modify-write such a run would otherwise do is the pre-v2 API key
		// sweep, which [Deps.complete] skips for exactly this reason.

	default:
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

	// The capped buffers stay the Result's source of truth whether or not anyone is
	// watching; streaming only forwards a copy of what they are being given, so the
	// live view and the final one agree about what was written.
	var out, errOut io.Writer = &stdout, &stderr
	if j.inv.Stream {
		so := &streamWriter{to: &stdout, limit: r.streamChunk}
		se := &streamWriter{to: &stderr, limit: r.streamChunk, stderr: true}
		out, errOut = so, se
		// Deferred, so the last of the output is forwarded before execute returns
		// and the run is reported done.
		defer r.stream(id, j.inv, so, se)()
	}

	// The run has no terminal, so its questions are asked in the UI. Everything
	// else a Prompter reads still needs one, and is refused. Its writer is errOut,
	// not the capped buffer directly, so a question asked mid-stream reaches the
	// pane the same way the command's own output does, instead of only surfacing
	// once the run is done.
	deps.Prompt = prompt.New(in, errOut, prompt.WithConfirmer(r.confirmer(ctx, id, j)))

	started := time.Now()
	// The command line is run exactly as given; the run's Deps carries what the UI
	// decides.
	code := executeRoot(ctx, deps, newRootCmd(deps), j.inv.Args, in, out, errOut)
	if j.rescansComponents && code == exitcode.OK {
		r.rescanComponents()
	}

	res := tui.Result{
		ExitCode:        code,
		Status:          int(status.Load()),
		Stdout:          stdout.buf,
		Stderr:          stderr.buf,
		StdoutTruncated: stdout.dropped,
		StderrTruncated: stderr.dropped,
		Duration:        time.Since(started),
		Recorded:        deps.run.recorded.Load(),
	}
	if p := deps.run.project.Load(); p != nil {
		res.Project = *p
	}
	return res
}

// rescanComponents reads the component root again, so that the runs after an
// install, an upgrade or a removal are given what is there now.
//
// A failure leaves the previous registry in place. [component.Open] cannot tell
// "the root is empty" from "the root could not be read" on its own: a real read
// error still comes back as a registry that only knows the first-party table,
// recorded instead as a [component.Problem] on the root itself. Storing that
// unconditionally would make every installed component vanish from the UI
// because one directory listing happened to fail — so a rescan that carries such
// a Problem is discarded, and the last good registry stays in place.
func (r *cliRunner) rescanComponents() {
	r.rescanMu.Lock()
	defer r.rescanMu.Unlock()

	old := r.components.Load()
	if old == nil {
		return
	}
	root := old.Root()
	if root == "" {
		// Components are disabled for this process; there is nothing to read.
		return
	}

	next := component.Open(root)
	if slices.ContainsFunc(next.Problems(), func(p component.Problem) bool { return p.Dir == root }) {
		return
	}
	r.components.Store(next)
}

// streamWriter passes everything through to the run's capped buffer, and keeps a
// copy of the most recent output for the UI to be given between flushes.
//
// It holds the newest bytes rather than the oldest: what a live log shows is its
// tail, and everything written is in the capped buffer regardless.
type streamWriter struct {
	to     io.Writer
	limit  int
	stderr bool

	// mu guards the pending output, which the run writes and the flush loop takes.
	mu      sync.Mutex
	pending []byte
	dropped bool
}

func (w *streamWriter) Write(p []byte) (int, error) {
	n, err := w.to.Write(p)

	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	if len(w.pending) > w.limit {
		w.pending = w.pending[len(w.pending)-w.limit:]
		w.dropped = true
	}
	return n, err
}

// take is the output written since the last take, and nil when there is none.
// The bytes are handed over rather than copied: the caller owns them, and this
// writer starts again from empty.
func (w *streamWriter) take() *tui.Chunk {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 && !w.dropped {
		return nil
	}
	c := &tui.Chunk{Stderr: w.stderr, Bytes: w.pending, Dropped: w.dropped}
	w.pending, w.dropped = nil, false
	return c
}

// drop records that a chunk never reached the UI, so that the next one that does
// says so.
func (w *streamWriter) drop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dropped = true
}

// stream forwards the output of run id every streamFlush, and returns the function
// that stops it — which flushes once more, after the command has finished writing.
func (r *cliRunner) stream(id tui.RunID, inv tui.Invocation, writers ...*streamWriter) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		t := time.NewTicker(r.streamFlush)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				r.flush(id, inv, writers)
			case <-done:
				return
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
		r.flush(id, inv, writers)
	}
}

// flush hands the UI whatever each writer has, or drops it and remembers to say
// so on the next one.
//
// A streaming run is never held up to deliver its output. Blocking here would
// stop a command mid-write because the UI is busy drawing, and the whole of what
// was written is in the run's Result either way.
func (r *cliRunner) flush(id tui.RunID, inv tui.Invocation, writers []*streamWriter) {
	for _, w := range writers {
		c := w.take()
		if c == nil {
			continue
		}
		ev := tui.RunEvent{ID: id, State: tui.RunRunning, Invocation: inv, At: time.Now(), Chunk: c}
		select {
		case r.events <- ev:
		default:
			w.drop()
		}
	}
}

// cappedBuffer keeps the first limit bytes written to it, and drops the rest.
//
// It bounds what it allocates as well as what it keeps. A bytes.Buffer grows by
// doubling, so one holding a few bytes under the limit may have reserved nearly
// twice it; this one never reserves more than limit bytes.
type cappedBuffer struct {
	buf     []byte
	limit   int
	dropped bool
}

// Write never fails, and always reports everything as written: a command whose
// output the UI has stopped keeping must still finish, and report how it ended.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	room := max(b.limit-len(b.buf), 0)
	kept := p[:min(room, len(p))]
	if len(kept) < len(p) {
		b.dropped = true
	}
	if need := len(b.buf) + len(kept); need > cap(b.buf) {
		grown := make([]byte, len(b.buf), b.grownCap(need))
		copy(grown, b.buf)
		b.buf = grown
	}
	b.buf = append(b.buf, kept...)
	return len(p), nil
}

// grownCap is the capacity to grow to for need bytes: double the current one, but
// never past limit. need never exceeds limit, since Write keeps no more than fits.
// The doubling is only taken below limit/2, so it cannot overflow.
func (b *cappedBuffer) grownCap(need int) int {
	if cap(b.buf) > b.limit/2 {
		return b.limit
	}
	return min(max(2*cap(b.buf), need), b.limit)
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
