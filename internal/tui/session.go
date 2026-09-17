package tui

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// runEventMsg carries one of the runner's events into Update.
type runEventMsg RunEvent

// runnerClosedMsg says the runner has shut down and will send nothing more.
type runnerClosedMsg struct{}

// waitForEvent delivers the runner's next event. Update asks for the one after it
// each time one arrives, so exactly one of these is ever waiting.
func waitForEvent(events <-chan RunEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return runnerClosedMsg{}
		}
		return runEventMsg(ev)
	}
}

// action is one command the UI can run, and the command line it stands for.
type action struct {
	inv Invocation

	// display is the fft command a user would type for the same effect. It is
	// never where a secret goes; neither is inv.Args.
	display shellCommand
}

// session is the state every screen shares: the runner, the project the UI acts
// on, and what is known about it.
//
// The model is a tree of pointers, updated only on Bubble Tea's event loop, so a
// run's completion callback may change the screen that started it directly.
type session struct {
	runner Runner
	now    func() time.Time

	// project is the project the UI selected, "" while fft's own resolution — the
	// active project, or the environment's — decides.
	project string

	// resolved is the project fft's own resolution picks, as `project list` last
	// reported it.
	resolved string

	// switches counts the UI's project selections. A run that reads something
	// about the current project notes it when it starts, and an answer that
	// arrives after a switch is about a project the UI has left.
	switches uint64

	// readOnlyFloor is fft tui --read-only or FFT_READ_ONLY: every project is
	// read-only for this session, whatever its configuration says.
	readOnlyFloor bool

	// headless is set when the config file is not fft's to change in this process:
	// the caller said so at the start, or `project list` has since reported the
	// environment's project.
	headless bool

	// startedHeadless is what the caller said, which a list without the
	// environment's project does not overrule.
	startedHeadless bool

	// projectReadOnly is whether the current project is configured read-only.
	projectReadOnly bool

	// status is the current project's credential state, nil until known.
	status *authStatus

	// grants is what the user's roles permit, nil until whoami has said. It
	// applies only while its project is the current one.
	grants *grants

	runs *runList
	done map[RunID]func(Result) tea.Cmd

	// questions are what running commands are waiting to be told, oldest first.
	// The first is the one asked; the others wait their turn.
	questions []*question

	// declined are the runs the user answered no, until the run's caller has
	// heard how it ended: a command told no fails, and that failure is the
	// user's choice rather than something that went wrong.
	declined map[RunID]bool

	// st draws the questions' dialogs.
	st styles

	// requests are the runs the Request screen sent, as they were sent, so that
	// the Response screen can send one again. They are forgotten with the run.
	requests map[RunID]*sentRequest

	// The editor seams: how a process takes over the terminal, where its
	// environment is read, and where its temporary files go.
	execProcess func(*exec.Cmd, tea.ExecCallback) tea.Cmd
	getenv      func(string) string
	environ     func() []string
	tempDir     string

	// tempDirs are the private directories an editor is still working in. Each is
	// removed, with everything the editor left in it, when its editor exits, and
	// whatever is left when the UI ends is removed then.
	tempDirs map[string]bool
}

// sentRequest is a request the Request screen sent: the operation, and the
// invocation with its body.
type sentRequest struct {
	op  Operation
	inv Invocation

	// project is the project it was pinned to, "" when fft's own resolution
	// chose. The run's result says which one that was.
	project string

	// from says where the body came from, "" for the form's own.
	from string
}

func newSession(opts Options, st styles) *session {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	execProcess := opts.execProcess
	if execProcess == nil {
		execProcess = tea.ExecProcess
	}
	getenv := opts.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	environ := opts.environ
	if environ == nil {
		environ = os.Environ
	}
	return &session{
		runner:        opts.Runner,
		now:           now,
		st:            st,
		execProcess:   execProcess,
		getenv:        getenv,
		environ:       environ,
		tempDir:       opts.tempDir,
		tempDirs:      make(map[string]bool),
		requests:      make(map[RunID]*sentRequest),
		project:       opts.Project,
		readOnlyFloor: opts.ReadOnly,
		// Known before the list is: a key pressed while it loads is refused as
		// surely as one pressed after.
		headless:        opts.Headless,
		startedHeadless: opts.Headless,
		runs:            newRunList(),
		done:            make(map[RunID]func(Result) tea.Cmd),
		declined:        make(map[RunID]bool),
	}
}

// currentProject is the name of the project the next run acts on, "" when none
// is known.
func (s *session) currentProject() string {
	if s.project != "" {
		return s.project
	}
	return s.resolved
}

// target is the project a request is pinned to: the one the UI names, so that it
// goes where the screen said it would. It is "" when fft's own resolution must
// decide — headless, where the environment names the project and the config file
// is not consulted, and before the UI has learnt which project is active.
func (s *session) target() string {
	if s.headless {
		return ""
	}
	return s.currentProject()
}

// named is how a question names the project a request goes to.
func (s *session) named() string {
	if p := s.currentProject(); p != "" {
		return p
	}
	return "the active project"
}

// readOnly reports whether writes to the current project are refused.
func (s *session) readOnly() bool {
	return s.readOnlyFloor || s.projectReadOnly
}

// selectProject makes name the project every later run acts on. What was known
// about the previous one no longer applies.
func (s *session) selectProject(name string) {
	s.project = name
	s.switches++
	s.status = nil
	s.grants = nil
	s.runner.SetProject(name)
}

// lacking is the permissions op wants that the user appears to hold none of on the
// current project, nil when they hold one or when nobody can tell. See [grants].
func (s *session) lacking(op Operation) []string {
	if s.grants == nil || s.grants.project != s.currentProject() {
		return nil
	}
	return s.grants.lacking(op)
}

// lackingNotes is what a question about sending op to project adds when the user
// appears to lack its permission there: nothing, or the one note. project is ""
// for the current one; the roles of any other are not known.
func (s *session) lackingNotes(op Operation, project string) []string {
	if project != "" && project != s.currentProject() {
		return nil
	}
	if note := lackingNote(s.lacking(op)); note != "" {
		return []string{note}
	}
	return nil
}

// scoped is the display of a command that acts on the current project: the
// --project a shell would need, since the UI's choice is not in its argv.
func (s *session) scoped(args ...string) action {
	return action{inv: Invocation{Args: args}, display: s.displayFor(args, s.project)}
}

// displayFor is args as a shell would need them to act on project: with the
// --project the UI decides beside the command line. The environment's project is
// the one a shell with the same environment reaches without it.
func (s *session) displayFor(args []string, project string) shellCommand {
	if project == "" || s.headless {
		return commandLine(args)
	}
	return commandLine(append(slices.Clone(args), "--project", project))
}

// start runs a, and calls done with its result once it has finished.
func (s *session) start(a action, done func(Result) tea.Cmd) tea.Cmd {
	_, cmd := s.launch(a, done)
	return cmd
}

// launch is start, and says which run it started: 0 when the runner refused it,
// in which case done has already been called.
func (s *session) launch(a action, done func(Result) tea.Cmd) (RunID, tea.Cmd) {
	id, err := s.runner.Start(a.inv)
	if err != nil {
		// Nothing ran. The caller hears about it the way it hears about any other
		// failure, so that no screen needs a second error path.
		return 0, done(Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())})
	}
	s.runs.add(id, a.display, s.now())
	s.done[id] = done
	// A request is kept only as long as its run is.
	for kept := range s.requests {
		if _, ok := s.runs.byID[kept]; !ok {
			delete(s.requests, kept)
		}
	}
	return id, nil
}

// cleanup removes the directories an editor was still working in.
func (s *session) cleanup() {
	for dir := range s.tempDirs {
		// Best effort, as the UI goes: there is nobody left to tell.
		_ = os.RemoveAll(dir)
		delete(s.tempDirs, dir)
	}
}

// handle applies a runner event, and returns whatever the finished run's caller
// wants to happen next.
func (s *session) handle(ev RunEvent) tea.Cmd {
	s.runs.update(ev)
	if ev.Question != nil {
		s.ask(ev)
	}
	if ev.State != RunDone {
		return nil
	}
	// A run that has ended is asking nothing any more: it was cancelled, or timed
	// out, while its question waited.
	s.questions = slices.DeleteFunc(s.questions, func(q *question) bool { return q.run == ev.ID })
	defer delete(s.declined, ev.ID)
	done, ok := s.done[ev.ID]
	if !ok {
		return nil
	}
	delete(s.done, ev.ID)
	return done(ev.Result)
}

// wasDeclined reports whether the user answered no to a question run id asked.
// It is known until the run's caller has been told how the run ended.
func (s *session) wasDeclined(id RunID) bool {
	return s.declined[id]
}

// question is a question a running command asked, and the dialog that asks it.
type question struct {
	run    RunID
	id     uint64
	dialog *armedDialog

	// yes is what the dialog was answered with, once it has been.
	yes bool
}

// ask queues the question ev carries.
func (s *session) ask(ev RunEvent) {
	e, known := s.runs.byID[ev.ID]
	if !known {
		// Not a run this UI started, so there is nobody to show it to who knows
		// what it is about; and silence would leave the run waiting for ever.
		s.runner.Answer(ev.ID, ev.Question.ID, false)
		return
	}
	q := &question{run: ev.ID, id: ev.Question.ID}
	yes := func() tea.Cmd {
		q.yes = true
		return nil
	}

	detail := fmt.Sprintf("Command #%d is waiting for your answer", ev.ID)
	if ev.Invocation.Project != "" {
		detail += ", on project " + ev.Invocation.Project
	}
	detail += "."

	// The command's question is about what it looked up, in its own words; the
	// body it acts with is shown under it, because the question may not say.
	var preview *bodyPreview
	if sent := s.requests[ev.ID]; sent != nil && sent.inv.Stdin != nil {
		preview = newBodyPreview(sent.inv.Stdin, sent.from)
	}

	var notes []string
	if sent := s.requests[ev.ID]; sent != nil {
		notes = s.lackingNotes(sent.op, sent.project)
	}

	var d dialog
	if word := ev.Question.Confirm; word != "" {
		typed := newTypeNameDialog(s.st, ev.Question.Text, detail+" It cannot be undone.", word, e.display, yes)
		typed.what = "word"
		typed.preview = preview
		typed.notes = notes
		d = typed
	} else {
		d = &confirmDialog{
			question: ev.Question.Text,
			notes:    notes,
			detail:   detail,
			command:  e.display,
			onYes:    yes,
			preview:  preview,
		}
	}
	// Armed only once it is in front of the user, which may be well after it came.
	q.dialog = armed(d, s.now, false)
	s.questions = append(s.questions, q)
}

// asking is the question the UI asks now, nil when no command is waiting.
func (s *session) asking() *question {
	if len(s.questions) == 0 {
		return nil
	}
	return s.questions[0]
}

// answerWith hands msg to the question being asked, and sends the answer once it
// has one.
func (s *session) answerWith(msg tea.Msg) tea.Cmd {
	q := s.asking()
	if q == nil {
		return nil
	}
	finished, cmd := q.dialog.update(msg)
	if !finished {
		return cmd
	}
	s.questions = s.questions[1:]
	if e, ok := s.runs.byID[q.run]; ok {
		e.asking = false
	}
	if !q.yes {
		s.declined[q.run] = true
	}
	s.runner.Answer(q.run, q.id, q.yes)
	return cmd
}

// failure is a run that did not succeed, as a screen shows it.
type failure struct {
	what   string
	result Result
}

// view renders the failure: what was being done, the exit code and what it means,
// and the tail of what the command said about it.
func (f failure) view(st styles, width int) string {
	var b strings.Builder
	b.WriteString(st.errorText.Render(fmt.Sprintf("%s failed: exit %d (%s)",
		output.SanitizeCell(f.what), f.result.ExitCode, exitcode.Meaning(f.result.ExitCode))))
	for _, line := range stderrTail(f.result.Stderr, 6) {
		b.WriteString("\n  ")
		b.WriteString(clip(line, width-2))
	}
	return b.String()
}

// stderrTail is the last n non-empty lines of a command's stderr, stripped of
// anything a terminal would act on: the text is the API's as much as fft's.
func stderrTail(stderr []byte, n int) []string {
	lines := make([]string, 0, n)
	for line := range strings.SplitSeq(output.Sanitize(string(stderr)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// authStatus is `fft auth status -o json`.
type authStatus struct {
	Project   string     `json:"project"`
	Store     string     `json:"store"`
	SignIn    string     `json:"signIn"`
	Token     string     `json:"token"`
	Expired   bool       `json:"expired"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// tokenSummary is the status bar's word on the current token.
func (a *authStatus) tokenSummary(now time.Time) string {
	if a == nil {
		return "token ?"
	}
	prefix := "token"
	if a.SignIn == "idToken" {
		prefix = "fixed token"
	}
	switch a.Token {
	case "none":
		switch {
		case a.SignIn == "none":
			return "no credentials"
		case a.Store == "env":
			// The environment's store keeps no token, so the state it reports says
			// nothing about the one this session holds in memory.
			return "token not stored (environment)"
		}
		return "not signed in"
	case "unknown":
		return prefix + " expiry unknown"
	case "expired":
		return prefix + " expired"
	}
	if a.ExpiresAt == nil {
		return prefix + " " + a.Token
	}
	left := a.ExpiresAt.Sub(now)
	if left <= 0 {
		return prefix + " expired"
	}
	return prefix + " " + remaining(left) + " left"
}

// remaining renders a token's remaining life to the minute, which is all a status
// bar needs.
func remaining(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
}
