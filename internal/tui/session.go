package tui

import (
	"fmt"
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
	display string
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

	// readOnlyFloor is fft tui --read-only or FFT_READ_ONLY: every project is
	// read-only for this session, whatever its configuration says.
	readOnlyFloor bool

	// headless is set once `project list` has reported the environment's project:
	// the config file is not fft's to change in this process.
	headless bool

	// projectReadOnly is whether the current project is configured read-only.
	projectReadOnly bool

	// status is the current project's credential state, nil until known.
	status *authStatus

	runs *runList
	done map[RunID]func(Result) tea.Cmd
}

func newSession(opts Options) *session {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &session{
		runner:        opts.Runner,
		now:           now,
		project:       opts.Project,
		readOnlyFloor: opts.ReadOnly,
		runs:          newRunList(),
		done:          make(map[RunID]func(Result) tea.Cmd),
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

// readOnly reports whether writes to the current project are refused.
func (s *session) readOnly() bool {
	return s.readOnlyFloor || s.projectReadOnly
}

// selectProject makes name the project every later run acts on. What was known
// about the previous one no longer applies.
func (s *session) selectProject(name string) {
	s.project = name
	s.status = nil
	s.runner.SetProject(name)
}

// scoped is the display of a command that acts on the current project: the
// --project a shell would need, since the UI's choice is not in its argv.
func (s *session) scoped(args ...string) action {
	display := commandLine(args)
	if s.project != "" && !s.headless {
		display += " --project " + shellQuote(s.project)
	}
	return action{inv: Invocation{Args: args}, display: display}
}

// start runs a, and calls done with its result once it has finished.
func (s *session) start(a action, done func(Result) tea.Cmd) tea.Cmd {
	id, err := s.runner.Start(a.inv)
	if err != nil {
		// Nothing ran. The caller hears about it the way it hears about any other
		// failure, so that no screen needs a second error path.
		return done(Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())})
	}
	s.runs.add(id, a.display, s.now())
	s.done[id] = done
	return nil
}

// handle applies a runner event, and returns whatever the finished run's caller
// wants to happen next.
func (s *session) handle(ev RunEvent) tea.Cmd {
	s.runs.update(ev)
	if ev.State != RunDone {
		return nil
	}
	done, ok := s.done[ev.ID]
	if !ok {
		return nil
	}
	delete(s.done, ev.ID)
	return done(ev.Result)
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
		f.what, f.result.ExitCode, exitcode.Meaning(f.result.ExitCode))))
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
		if a.SignIn == "none" {
			return "no credentials"
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
