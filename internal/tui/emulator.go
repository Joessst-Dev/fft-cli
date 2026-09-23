package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// emulatorPort is the port the emulator listens on unless it is told otherwise;
// it is `fft emulator`'s own default, repeated here because the UI has to name the
// base URL before the emulator has said anything.
const emulatorPort = 8080

// emulatorLogLines is how much of the emulator's output the pane keeps. It is a
// tail, not a transcript: the whole of it is in the run's result, and the run does
// not end until the emulator is stopped.
const emulatorLogLines = 200

// emulatorReady is what the emulator prints once the port is bound. It is printed
// by the ready callback, after the listen succeeds, so seeing it means the server
// is actually answering.
const emulatorReady = "fft emulator listening on"

// componentSource says what is known about the installed components. The
// componentsScreen implements it; the pane uses it so the emulator's install state
// is read once for the whole UI.
type componentSource interface {
	// component is the row for name, and whether the list has been read at all.
	component(name string) (componentRow, bool)

	// want reads the list unless it has been asked for already.
	want() tea.Cmd
}

// emulatorState is where the local emulator is.
type emulatorState int

const (
	emulatorStopped emulatorState = iota
	emulatorStarting
	emulatorRunning
	emulatorFailed
)

type emulatorKeys struct {
	toggle key.Binding
	copy   key.Binding
	close  key.Binding
}

func newEmulatorKeys() emulatorKeys {
	return emulatorKeys{
		toggle: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start/stop")),
		copy:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy the FFT_* recipe")),
		close:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back to the projects")),
	}
}

// emulatorPane runs the local offline emulator and shows what it is saying.
//
// It lives on the Projects screen because the emulator is a tenant you could be
// working against, not a seventh thing to send requests with. Running it from here
// is as far as this goes for now: pointing the session itself at the emulator needs
// a seam that does not exist yet — Deps.Ephemeral and the credential store are both
// rebuilt per run from the process environment (config.FromEnv, Deps.openSecrets),
// so the UI would have to be able to override that environment for its runs rather
// than read it. Until then the pane offers the recipe, and another shell uses it.
//
// The emulator holds one of the runner's slots for as long as it runs, which is why
// only one can be started from here.
type emulatorPane struct {
	s    *session
	st   styles
	keys emulatorKeys

	// components is where the pane learns whether the emulator is installed.
	components componentSource

	// open is whether the pane is what the Projects screen draws.
	open bool

	state emulatorState
	run   RunID
	port  int

	// log is the tail of what the emulator has printed, newest last.
	log []string

	// partial is the end of the last chunk, which need not be a whole line.
	partial string

	notice  string
	failure *failure
}

func newEmulatorPane(s *session, st styles) *emulatorPane {
	return &emulatorPane{s: s, st: st, keys: newEmulatorKeys(), port: emulatorPort}
}

// installed reports whether the emulator component is there, and whether the
// component list has been read at all.
func (e *emulatorPane) installed() (yes, known bool) {
	if e.components == nil {
		return false, false
	}
	row, read := e.components.component(emulatorComponent)
	if !read {
		return false, false
	}
	return row.installed(), true
}

func (e *emulatorPane) running() bool {
	return e.state == emulatorStarting || e.state == emulatorRunning
}

func (e *emulatorPane) baseURL() string {
	return fmt.Sprintf("http://localhost:%d", e.port)
}

// recipe is the environment that points another shell at the emulator. It is
// computed rather than read out of the emulator's output: the emulator prints the
// same four lines, and they follow from the port alone.
func (e *emulatorPane) recipe() string {
	return strings.Join([]string{
		"export " + config.EnvBaseURL + "=" + e.baseURL(),
		"export " + config.EnvFirebaseAPIKey + "=emulator",
		"export " + config.EnvEmail + "=dev@localhost",
		"export " + config.EnvIDToken + "=emulator-token",
	}, "\n")
}

func (e *emulatorPane) startArgs() []string {
	return []string{"emulator", "--port", strconv.Itoa(e.port)}
}

func (e *emulatorPane) update(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, e.keys.close):
		e.open = false
	case key.Matches(msg, e.keys.toggle):
		return e.toggle()
	case key.Matches(msg, e.keys.copy):
		return tea.SetClipboard(e.recipe())
	}
	return nil
}

func (e *emulatorPane) toggle() tea.Cmd {
	if e.running() {
		// Cancelling sends the child an interrupt, which is how the emulator is meant
		// to be stopped: it drains its transports and shuts the server down.
		e.s.runner.Cancel(e.run)
		e.notice = "Stopping the emulator…"
		return nil
	}
	return e.start()
}

func (e *emulatorPane) start() tea.Cmd {
	if yes, known := e.installed(); known && !yes {
		e.notice = ""
		e.failure = nil
		e.notice = "The emulator is not installed. Press 8 for Components, then a to install it."
		return nil
	}

	args := e.startArgs()
	a := action{inv: Invocation{Args: args, Stream: true}, display: commandLine(args)}
	e.state, e.log, e.partial = emulatorStarting, nil, ""
	e.failure = nil
	e.notice = "Starting the emulator…"

	var id RunID
	id, cmd := e.s.launch(a, func(r Result) tea.Cmd {
		if e.run != id {
			// A later start owns the pane now; this one's ending says nothing about it.
			return nil
		}
		e.run = 0
		switch {
		case r.ExitCode == exitcode.OK, r.ExitCode == exitcode.Interrupted:
			// Interrupted is how a server that was told to stop ends.
			e.state = emulatorStopped
			e.notice = "The emulator has stopped."
			e.failure = nil
		default:
			e.state = emulatorFailed
			e.notice = ""
			e.failure = &failure{what: "running the emulator", result: r}
		}
		return nil
	})
	e.run = id
	e.s.watch(id, e.observe)
	return cmd
}

// observe takes a chunk of the emulator's output. Both streams go to the same log:
// the emulator says everything it has to say on stderr, and a line that did arrive
// on stdout is still something the user should see where they are looking.
func (e *emulatorPane) observe(c *Chunk) {
	if c.Dropped {
		e.append("… some output was dropped while the screen was busy …")
	}
	text := e.partial + string(c.Bytes)
	lines := strings.Split(text, "\n")
	// The last piece has no newline after it yet, and is held until it does.
	e.partial = lines[len(lines)-1]
	for _, line := range lines[:len(lines)-1] {
		e.append(line)
		if e.state == emulatorStarting && strings.Contains(line, emulatorReady) {
			e.state = emulatorRunning
			e.notice = "The emulator is listening on " + e.baseURL() + "."
		}
	}
}

func (e *emulatorPane) append(line string) {
	e.log = append(e.log, output.SanitizeCell(strings.TrimRight(line, "\r")))
	if len(e.log) > emulatorLogLines {
		e.log = e.log[len(e.log)-emulatorLogLines:]
	}
}

func (e *emulatorPane) bindings() []key.Binding {
	return []key.Binding{e.keys.toggle, e.keys.copy, e.keys.close}
}

func (e *emulatorPane) legend() legendSection {
	return legendSection{
		title:   "In the emulator pane",
		compact: true,
		entries: []legendEntry{
			{of: e.keys.toggle, desc: "run the emulator, or stop the one running"},
			{of: e.keys.copy, desc: "copy the FFT_* recipe that points a shell at it"},
			{of: e.keys.close, desc: "back to the projects"},
		},
	}
}

func (e *emulatorPane) equivalent() shellCommand {
	return commandLine(e.startArgs())
}

func (e *emulatorPane) stateText() string {
	switch e.state {
	case emulatorStarting:
		return "starting"
	case emulatorRunning:
		return "running"
	case emulatorFailed:
		return "failed"
	}
	return "stopped"
}

func (e *emulatorPane) view(width, height int) string {
	st := e.st
	lines := []string{st.title.Render("Emulator"), ""}

	status := "Status: " + e.stateText()
	switch e.state {
	case emulatorRunning:
		lines = append(lines, st.okText.Render(status))
	case emulatorFailed:
		lines = append(lines, st.errorText.Render(status))
	default:
		lines = append(lines, st.dim.Render(status))
	}

	yes, known := e.installed()
	switch {
	case !known:
		lines = append(lines, st.dim.Render("Component: reading the list…"))
	case yes:
		lines = append(lines, st.dim.Render("Component: installed"))
	default:
		lines = append(lines, st.warnText.Render(
			"Component: not installed — press 8 for Components, then a to install it."))
	}

	lines = append(lines, "", st.dim.Render("Point another shell at it:"))
	for _, line := range strings.Split(e.recipe(), "\n") {
		lines = append(lines, "  "+line)
	}

	lines = append(lines, "")
	if e.notice != "" {
		lines = append(lines, wrap(st.okText.Render(output.SanitizeCell(e.notice)), width))
	}
	if e.failure != nil {
		lines = append(lines, e.failure.view(st, width))
	}

	// Whatever rows are left go to the log, newest last.
	lines = append(lines, st.dim.Render("Output"))
	room := max(height-len(lines), 0)
	log := e.log
	if len(log) > room {
		log = log[len(log)-room:]
	}
	if len(log) == 0 && room > 0 {
		log = []string{st.dim.Render("(nothing yet)")}
	}
	for _, line := range log {
		lines = append(lines, clip(line, width))
	}
	return strings.Join(lines, "\n")
}
