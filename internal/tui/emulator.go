package tui

import (
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

// emulatorPartialLimit bounds a line that never gets its newline. The emulator's
// own output is complete lines, so this is not reached today, but a partial that
// grew without bound while nothing terminated it would still have to be shown
// eventually — the same tail-not-transcript reasoning as emulatorLogLines.
const emulatorPartialLimit = 16 << 10

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
// working against, not a seventh thing to send requests with — and the screen's
// emulator row is how a session comes to work against it. Pointing the session is
// the row's; starting, stopping and the output are the pane's, and the two meet
// here: a row that asks for a session that is not running yet sets useWhenReady,
// and the line that says the port is bound is what points it.
//
// The recipe stays, because a second shell is still a reasonable thing to want, and
// it is the same four variables the runs are given ([config.EmulatorEnv]).
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

	// useWhenReady says the session is to be pointed at this emulator as soon as it
	// reports the port bound. Set by the Projects screen's emulator row, which is
	// asked to use an emulator that is not running yet.
	//
	// armedAt is session.selections when that was asked for — the count of the
	// user's own project/emulator choices, not [session.switches], which also
	// advances for a project removed out from under the current selection and
	// would cancel this arm on a change the user never made. A server can take
	// seconds to bind, and a user who changes their mind meanwhile and picks a
	// project bumps selections — so the ready line, arriving after, must not
	// quietly move the session back. What was asked for last wins.
	useWhenReady bool
	armedAt      uint64

	// afterUse is what the Projects screen wants done once the session has been
	// pointed: say so, and read the credential state again, which is now the
	// environment's rather than a configured project's.
	afterUse func() tea.Cmd

	// afterStop is called when the emulator ends while the session is still pointed
	// at it, so that the list behind the pane stops saying the session is using an
	// emulator that is answering.
	afterStop func()

	// log is the tail of what the emulator has printed, newest last.
	log []string

	// partialOut and partialErr are the end of the last chunk on each stream, which
	// need not be a whole line. Kept apart because stdout and stderr are written by
	// different goroutines in the child: joining an unfinished fragment from one
	// with the next chunk from the other would show a line that was never actually
	// written.
	partialOut, partialErr string

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
	return config.EmulatorBaseURL(e.port)
}

// recipe is the environment that points another shell at the emulator. It is
// computed rather than read out of the emulator's output: the emulator prints the
// same four lines, and they follow from the port alone.
func (e *emulatorPane) recipe() string {
	lines := make([]string, 0, 4)
	for _, v := range config.EmulatorEnv(e.baseURL()) {
		lines = append(lines, "export "+v.Name+"="+v.Value)
	}
	return strings.Join(lines, "\n")
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
	cmd, _ := e.start()
	return cmd
}

// use points the session at the emulator, starting it first if it is not running.
// It reports what the user should be told about that, and the work it started.
func (e *emulatorPane) use() (notice string, f *failure, cmd tea.Cmd) {
	if e.running() {
		switch {
		case e.s.usingEmulator():
			return "This session is already using the emulator.", nil, nil
		case e.state == emulatorRunning:
			return "", nil, e.useNow()
		}
		// Still binding the port: the ready line points the session, as it would have.
		e.arm()
		return "The emulator is starting; this session will use it once it is listening.", nil, nil
	}

	// Not running — and that includes an emulator the session is still pointed at,
	// because stopping one leaves the session where it was. Pressing enter on the row
	// then is exactly the request to have it answering again, so it is started rather
	// than reported as already in use.
	again := e.s.usingEmulator()
	cmd, started := e.start()
	if !started {
		// Nothing runs, so nothing will ever be ready. Why is the pane's to say — the
		// component is not installed, or the runner refused the run.
		return e.notice, e.failure, cmd
	}
	e.arm()

	// There is output to watch now, so show where it goes rather than leave the user
	// on a list that only says it is starting. The component list is read with it,
	// which is how a session that has not looked yet comes to be told that the
	// emulator is not installed after all.
	e.open = true
	if e.components != nil {
		cmd = tea.Batch(e.components.want(), cmd)
	}
	if again {
		return "Starting the emulator again; this session is pointed at it.", nil, cmd
	}
	return "Starting the emulator; this session will use it once it is listening.", nil, cmd
}

// arm asks for the session to be pointed at this emulator once it reports the port
// bound, and records the selection that asked; see [emulatorPane.useWhenReady].
func (e *emulatorPane) arm() {
	e.useWhenReady, e.armedAt = true, e.s.selections
}

// useNow points the session at an emulator that is already listening.
func (e *emulatorPane) useNow() tea.Cmd {
	e.useWhenReady = false
	e.s.selectEmulator(e.baseURL())
	e.notice = "The emulator is listening on " + e.baseURL() + "; this session is using it."
	if e.afterUse == nil {
		return nil
	}
	return e.afterUse()
}

// start runs the emulator, and reports whether anything was started. Nothing is
// when the component is not installed: there is then no run to wait on.
func (e *emulatorPane) start() (tea.Cmd, bool) {
	if yes, known := e.installed(); known && !yes {
		e.useWhenReady = false
		e.failure = nil
		e.notice = "The emulator is not installed. Press 8 for Components, then a to install it."
		return nil, false
	}

	args := e.startArgs()
	a := action{inv: Invocation{Args: args, Stream: true}, display: commandLine(args)}
	e.state, e.log, e.partialOut, e.partialErr = emulatorStarting, nil, "", ""
	e.failure = nil
	e.notice = "Starting the emulator…"

	var id RunID
	id, cmd := e.s.launch(a, func(r Result) tea.Cmd {
		if e.run != id {
			// A later start owns the pane now; this one's ending says nothing about it.
			return nil
		}
		e.run = 0
		// Nothing observes this run again after it is done, so whatever each stream
		// was still holding has to be shown now or not at all.
		e.flush()
		switch {
		case r.ExitCode == exitcode.OK, r.ExitCode == exitcode.Interrupted:
			// Interrupted is how a server that was told to stop ends.
			e.state = emulatorStopped
			e.notice = "The emulator has stopped."
			e.failure = nil
			if e.s.usingEmulator() {
				// The session is left where it is. Moving it back on the emulator's way
				// out would change what the next request reaches without the user asking,
				// and what they asked for was to stop a server, not to talk to a tenant.
				e.notice = "The emulator has stopped, and this session is still pointed at it: " +
					"press s to start it again, or choose a project on the Projects screen."
			}
		default:
			// The failure stands, whether or not the session is pointed here. A restart
			// that could not bind the port has something to say, and "it has stopped" is
			// not it: saying that instead would leave the user with no way to find out
			// why pressing enter did nothing.
			e.state = emulatorFailed
			e.notice = ""
			e.failure = &failure{what: "running the emulator", result: r}
		}
		e.useWhenReady = false
		if e.s.usingEmulator() && e.afterStop != nil {
			// Either way the list behind the pane must stop saying the session is using
			// an emulator that is answering.
			e.afterStop()
		}
		return nil
	})
	e.run = id
	e.s.watch(id, e.observe)
	return cmd, e.state == emulatorStarting
}

// observe takes a chunk of the emulator's output. Both streams go to the same log:
// the emulator says everything it has to say on stderr, and a line that did arrive
// on stdout is still something the user should see where they are looking.
func (e *emulatorPane) observe(c *Chunk) tea.Cmd {
	if c.Dropped {
		e.append("… some output was dropped while the screen was busy …")
	}
	partial := &e.partialOut
	if c.Stderr {
		partial = &e.partialErr
	}
	text := *partial + string(c.Bytes)
	lines := strings.Split(text, "\n")
	// The last piece has no newline after it yet, and is held until it does.
	*partial = lines[len(lines)-1]
	var cmd tea.Cmd
	for _, line := range lines[:len(lines)-1] {
		e.append(line)
		if e.state == emulatorStarting && strings.Contains(line, emulatorReady) {
			e.state = emulatorRunning
			e.notice = "The emulator is listening on " + e.baseURL() + "."
			if e.useWhenReady {
				// Only now: the line is printed by the ready callback, after the listen
				// succeeded, so pointing the session here means it is pointed at a port
				// that answers. And only if nothing has been selected since it was asked
				// for — a user who started the emulator and then chose a project meant
				// the project. selections, not switches: an unrelated invalidation (a
				// project removed out from under the current selection, say) must not
				// silently cancel an arm the user never contradicted.
				stillWanted := e.armedAt == e.s.selections
				e.useWhenReady = false
				if stillWanted {
					cmd = e.useNow()
				}
			}
		}
	}
	if len(*partial) > emulatorPartialLimit {
		// A fragment this long is not going to complete into a line worth waiting
		// for; show what there is rather than let it grow without bound.
		e.append(*partial)
		*partial = ""
	}
	return cmd
}

// flush shows whatever each stream's partial line still holds. Called once the run
// has ended, since observe otherwise never learns that no more bytes are coming.
func (e *emulatorPane) flush() {
	for _, p := range []*string{&e.partialOut, &e.partialErr} {
		if *p != "" {
			e.append(*p)
			*p = ""
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
			{of: e.keys.copy, desc: "copy the FFT_* recipe that points another shell at it"},
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

	session := "Session: not using it — press 1, then enter on the emulator row"
	style := st.dim
	if e.s.usingEmulator() {
		session, style = "Session: using it — every request goes to "+e.s.emulator, st.okText
	}
	lines = append(lines, style.Render(session))

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
