package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// headlessExplanation is why the Projects screen changes nothing in headless mode.
// It says what `project add` and its siblings would say, before sending them.
const headlessExplanation = "fft is running from the environment (FFT_BASE_URL is set), so projects " +
	"cannot be added, switched, changed or removed here. Unset the FFT_* variables to manage the config file."

// emulatorExplanation is why the Projects screen changes nothing about the
// emulator row. It is not the headless one: unsetting FFT_* would not help, because
// the emulator was never in the config file to begin with.
const emulatorExplanation = "the emulator is not a configured project — it is the local server the " +
	"UI starts, and it is never written to the config file. Press enter to work against it, " +
	"or e for the pane that runs it."

// projectRow is one entry of `fft project list -o json`, or the row the UI adds
// for the emulator.
type projectRow struct {
	Name       string `json:"name"`
	Active     bool   `json:"active"`
	BaseURL    string `json:"baseUrl"`
	Email      string `json:"email"`
	Credential string `json:"credential"`
	ReadOnly   bool   `json:"readOnly"`
	Ephemeral  bool   `json:"ephemeral"`

	// emulator marks the row the UI adds rather than one fft listed, and is never
	// decoded: `fft project list` reads the config file, and the emulator is not in
	// it — nor may it ever be, since it cannot be signed in to.
	emulator bool
}

type projectKeys struct {
	up       key.Binding
	down     key.Binding
	use      key.Binding
	readOnly key.Binding
	remove   key.Binding
	refresh  key.Binding
	add      key.Binding
	reload   key.Binding
	emulator key.Binding
}

func newProjectKeys() projectKeys {
	return projectKeys{
		up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "select")),
		down:     key.NewBinding(key.WithKeys("down", "j")),
		use:      key.NewBinding(key.WithKeys("enter", "u"), key.WithHelp("enter", "use")),
		readOnly: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "read-only on/off")),
		remove:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove")),
		refresh:  key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh token")),
		add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		reload:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reload")),
		emulator: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "emulator")),
	}
}

// projectsScreen lists the configured projects and manages them, each action
// through the fft command that does it.
type projectsScreen struct {
	s    *session
	st   styles
	keys projectKeys

	rows   []projectRow
	cursor int

	// listed is how many rows fft returned, which the emulator row the UI adds is
	// not one of: "no projects are configured" is a statement about the config file.
	listed int

	loaded  bool
	warmed  bool
	notice  string
	refused bool

	// refusedEmulator is the emulator row's counterpart to refused: a change refused
	// because the row is not in the config file, rather than because this process
	// may not write to it.
	refusedEmulator bool

	failure *failure

	dialog dialog
	form   *addForm

	// emulator runs the local offline tenant. It is a pane of this screen rather
	// than a screen of its own: what it offers is another thing to work against.
	emulator *emulatorPane
}

func newProjectsScreen(s *session, st styles) *projectsScreen {
	p := &projectsScreen{s: s, st: st, keys: newProjectKeys(), emulator: newEmulatorPane(s, st)}
	// What follows the pane pointing the session, whether the user asked from the
	// row or the emulator became ready a moment later: say so where the row is, and
	// read the credential state again — it is the environment's now, not a
	// configured project's.
	p.emulator.afterUse = func() tea.Cmd {
		p.succeed("Now using the emulator at " + p.emulator.baseURL() + ".")
		p.syncCurrent()
		return p.refreshStatus(false)
	}
	// The list behind the pane said the session was using the emulator; once it has
	// stopped, only the warning above the table is still true.
	p.emulator.afterStop = func() { p.notice = "" }
	return p
}

// components is where the emulator pane learns whether the emulator is installed;
// see [componentSource].
func (p *projectsScreen) setComponents(src componentSource) {
	p.emulator.components = src
}

// init loads the list and the current project's credential state.
func (p *projectsScreen) init() tea.Cmd {
	return tea.Batch(p.reload(), p.refreshStatus(true))
}

func (p *projectsScreen) focused() bool { return p.dialog != nil || p.form != nil }

func (p *projectsScreen) selected() (projectRow, bool) {
	if len(p.rows) == 0 {
		return projectRow{}, false
	}
	p.cursor = min(max(p.cursor, 0), len(p.rows)-1)
	return p.rows[p.cursor], true
}

func (p *projectsScreen) update(msg tea.Msg) tea.Cmd {
	if d := p.dialog; d != nil {
		finished, cmd := d.update(msg)
		// An answer may have opened the next question; only this one is closed.
		if finished && p.dialog == d {
			p.dialog = nil
		}
		return cmd
	}
	if p.form != nil {
		switch ev, cmd := p.form.update(msg); ev {
		case formCancelled:
			p.form = nil
			return nil
		case formSubmitted:
			return p.submitForm()
		default:
			return cmd
		}
	}

	keyMsg, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return nil
	}
	return p.listKey(keyMsg)
}

// listKey handles a key on the list, with neither a dialog nor the form open.
func (p *projectsScreen) listKey(msg tea.KeyPressMsg) tea.Cmd {
	if p.emulator.open {
		// The pane is what is drawn, so it is what the keys act on: an a here must not
		// open the add form over a list nobody can see.
		return p.emulator.update(msg)
	}
	row, ok := p.selected()
	switch {
	case key.Matches(msg, p.keys.emulator):
		p.emulator.open = true
		// Read once, so the pane can say whether the emulator is installed.
		if p.emulator.components != nil {
			return p.emulator.components.want()
		}
		return nil
	case key.Matches(msg, p.keys.up):
		p.cursor--
		p.selected()
	case key.Matches(msg, p.keys.down):
		p.cursor++
		p.selected()
	case key.Matches(msg, p.keys.reload):
		return tea.Batch(p.reload(), p.refreshStatus(false))
	case key.Matches(msg, p.keys.refresh):
		return p.refreshToken()
	case key.Matches(msg, p.keys.add):
		if p.refuseHeadless() {
			return nil
		}
		p.form = newAddForm(p.st)
	case !ok:
		return nil
	case key.Matches(msg, p.keys.use):
		switch {
		case row.emulator:
			return p.useEmulator()
		case p.s.usingEmulator() && p.s.headless:
			// Leaving the emulator is not a change to the config file, so headless mode
			// has nothing to refuse: the environment already names the project fft goes
			// back to, and there is no `project use` to run.
			return p.leaveEmulator()
		case p.refuseHeadless():
			return nil
		}
		return p.use(row.Name)
	case key.Matches(msg, p.keys.readOnly):
		if p.refuseEmulator(row) || p.refuseHeadless() {
			return nil
		}
		p.dialog = armed(p.readOnlyDialog(row), p.s.now, true)
	case key.Matches(msg, p.keys.remove):
		if p.refuseEmulator(row) || p.refuseHeadless() {
			return nil
		}
		p.dialog = armed(p.removeDialog(row), p.s.now, true)
	}
	return nil
}

// useEmulator points the session at the emulator, starting it first if it is not
// already listening.
func (p *projectsScreen) useEmulator() tea.Cmd {
	// The pane owns the emulator's lifetime, and says what it did; the row only
	// shows it where the user pressed the key.
	notice, f, cmd := p.emulator.use()
	if f != nil {
		p.notice, p.refused, p.refusedEmulator, p.failure = "", false, false, f
		return cmd
	}
	if notice != "" {
		p.succeed(notice)
	}
	return cmd
}

// leaveEmulator points the session back at the project fft's own resolution picks.
func (p *projectsScreen) leaveEmulator() tea.Cmd {
	p.s.selectEmulator("")
	p.syncCurrent()
	p.succeed("No longer using the emulator.")
	return p.refreshStatus(false)
}

// refuseEmulator stops a change to a row that is not in the config file, and says
// why. Nothing is sent.
func (p *projectsScreen) refuseEmulator(row projectRow) bool {
	if !row.emulator {
		return false
	}
	p.notice = ""
	p.refusedEmulator = true
	return true
}

// refuseHeadless stops a change to the config file that headless mode would
// refuse anyway, and says why. Nothing is sent.
func (p *projectsScreen) refuseHeadless() bool {
	if !p.s.headless {
		return false
	}
	p.notice = ""
	p.refused = true
	return true
}

func (p *projectsScreen) succeed(notice string) {
	p.notice = notice
	p.refused, p.refusedEmulator = false, false
	p.failure = nil
}

func (p *projectsScreen) fail(what string, r Result) {
	p.notice = ""
	p.failure = &failure{what: what, result: r}
}

func (p *projectsScreen) reload() tea.Cmd {
	args := []string{"project", "list"}
	return p.s.start(action{inv: Invocation{Args: args}, display: commandLine(args)}, func(r Result) tea.Cmd {
		if r.ExitCode != exitcode.OK {
			p.fail("listing the projects", r)
			return nil
		}
		var rows []projectRow
		if err := json.Unmarshal(r.Stdout, &rows); err != nil {
			p.fail("reading the project list", Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())})
			return nil
		}
		p.setRows(rows)
		return nil
	})
}

func (p *projectsScreen) setRows(rows []projectRow) {
	first := !p.loaded
	p.listed = len(rows)
	p.rows, p.loaded = append(rows, p.emulatorRow()), true

	// From what fft listed, never from the row the UI added: the emulator says
	// nothing about which project fft resolves, or about whether the config file is
	// this process's to change.
	p.s.resolved, p.s.headless = "", p.s.startedHeadless
	for _, r := range rows {
		if r.Active {
			p.s.resolved = r.Name
		}
		if r.Ephemeral {
			p.s.headless = true
		}
	}

	if i := p.syncCurrent(); first && i >= 0 {
		p.cursor = i
	}
	p.selected()
}

// emulatorRow is the local emulator, as the list shows it. It is composed, not
// read: the emulator is not in the config file, so nothing lists it, and everything
// about it follows from the port the pane would start it on.
func (p *projectsScreen) emulatorRow() projectRow {
	row := projectRow{
		Name:    "emulator",
		BaseURL: p.emulator.baseURL(),
		// The environment-backed store, which is what a run pointed here reads its
		// credential from and what `fft auth status` then reports. ReadOnly is left
		// false: the session's floor is the only thing that protects a local server
		// that is thrown away, and the table already applies it to every row.
		Credential: "env",
		emulator:   true,
	}
	// Read out of what the runs are actually given, rather than written again here.
	for _, v := range config.EmulatorEnv(row.BaseURL) {
		if v.Name == config.EnvEmail {
			row.Email = v.Value
		}
	}
	return row
}

// selectProject makes name the UI's project, and carries over what the list
// already says about it rather than waiting for the list to be read again.
func (p *projectsScreen) selectProject(name string) {
	p.s.selectProject(name)
	p.syncCurrent()
}

// syncCurrent copies the current project's read-only mark from the list, and
// returns its row, or -1 when the list does not have it.
func (p *projectsScreen) syncCurrent() int {
	current := p.s.currentProject()
	p.s.projectReadOnly = false
	for i, r := range p.rows {
		// Never the emulator's row, whose name is the UI's label rather than one out
		// of the config file: a configured project that happens to be called emulator
		// is a different thing that lists under the same word.
		if !r.emulator && r.Name == current {
			p.s.projectReadOnly = r.ReadOnly
			return i
		}
	}
	return -1
}

// refreshStatus reads the current project's credential state. At startup it also
// signs in, if the next authenticated run would have to: see [projectsScreen.warmUp].
func (p *projectsScreen) refreshStatus(startup bool) tea.Cmd {
	a := p.s.scoped("auth", "status")
	asked := p.s.switches
	return p.s.start(a, func(r Result) tea.Cmd {
		// First, before anything is cleared or reported: an answer read before a
		// switch is about the project the UI left, and the switch reads the state
		// again. Neither its success nor its failure may touch what that read says.
		if p.s.switches != asked {
			return nil
		}
		p.s.status = nil
		switch r.ExitCode {
		case exitcode.OK:
		case exitcode.Config:
			// No project to report on; the list says so better than an error would.
			return nil
		default:
			p.fail("reading the credential state", r)
			return nil
		}
		var st authStatus
		if err := json.Unmarshal(r.Stdout, &st); err != nil {
			p.fail("reading the credential state", Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())})
			return nil
		}
		if cur := p.s.currentProject(); cur != "" && st.Project != cur {
			// fft's own resolution moved under a read the UI did not switch for — the
			// active project removed, say. What moved it reads the state again.
			return nil
		}
		p.s.status = &st
		p.s.signIns++
		if startup && !p.warmed && needsSignIn(&st) {
			return p.warmUp()
		}
		return nil
	})
}

// needsSignIn reports whether the next authenticated run would mint a token. The
// session keeps what it mints for every run after it, the environment's projects
// included, whose store keeps nothing: a fixed id token is the only credential
// there is nothing to mint for.
func needsSignIn(st *authStatus) bool {
	return st.SignIn == "password" && st.Token != "valid"
}

// warmUp signs in to the current project with one run that has the runner to
// itself.
//
// The runs share the session's token, but the first of several started side by
// side would still sign in while the others wait on it — and a keychain prompt, if
// the system raises one, would appear under whatever the user happened to start.
// One run first, alone, puts both at a moment the user can connect to what they
// just did.
func (p *projectsScreen) warmUp() tea.Cmd {
	p.warmed = true
	a := p.s.scoped("auth", "whoami")
	a.inv.Exclusive = true
	// The user asked for a switch, not for this request: history leaves it out.
	a.inv.Background = true
	// Named now: by the time it answers, the UI may have switched on, and a failure
	// must name the project that failed, not the one in use.
	name := p.s.currentProject()
	return p.s.start(a, func(r Result) tea.Cmd {
		if r.ExitCode != exitcode.OK {
			p.fail("signing in to "+name, r)
		}
		return p.refreshStatus(false)
	})
}

// projectArgs is `fft project <verb> <name> <flags>`. A project name comes out
// of the config file, not user typing, so one starting with a dash (fft refuses
// to create such a project, but an older fft or a hand-edited config may have
// one) goes last, after "--", where it is only a name and never a flag — the
// same guard templateArgs uses for template names.
func projectArgs(verb, name string, flags ...string) []string {
	args := []string{"project", verb}
	if strings.HasPrefix(name, "-") {
		return append(append(append(args, flags...), "--"), name)
	}
	return append(append(args, name), flags...)
}

func (p *projectsScreen) useAction(name string) action {
	args := projectArgs("use", name)
	return action{inv: Invocation{Args: args, Exclusive: true}, display: commandLine(args)}
}

// use makes name the active project and the one the UI acts on, then signs in to
// it before anything else can run.
func (p *projectsScreen) use(name string) tea.Cmd {
	p.succeed("Switching to " + name + "…")
	// project use is exclusive and can queue behind a run already holding the
	// config file's read lock, so it can take a while — long enough for the user
	// to change their mind and press enter on the emulator row instead. Recording
	// the selection count now, and checking it below, is the same staleness guard
	// the emulator's own arm uses against the mirror-image race.
	asked := p.s.selections
	return p.s.start(p.useAction(name), func(r Result) tea.Cmd {
		if r.ExitCode != exitcode.OK {
			// A refused switch changed nothing, so there is nothing to reload.
			p.fail("switching to "+name, r)
			return nil
		}
		if p.s.selections != asked {
			// The session has moved on to something else since this was asked for;
			// selecting the project now would silently move it back. The list is
			// still worth a reload, since fft's own active project did change.
			return p.reload()
		}
		p.selectProject(name)
		p.succeed("Now using " + name + ".")
		return tea.Batch(p.reload(), p.warmUp())
	})
}

func (p *projectsScreen) readOnlyDialog(row projectRow) dialog {
	if row.ReadOnly {
		return p.allowWritesDialog(row)
	}
	args := projectArgs("read-only", row.Name)
	a := action{inv: Invocation{Args: args, Exclusive: true}, display: commandLine(args)}
	return &confirmDialog{
		question: fmt.Sprintf("Make %s read-only?", row.Name),
		detail:   "fft will refuse every request that would change it.",
		command:  a.display,
		onYes: func() tea.Cmd {
			return p.startReadOnly(a, row.Name, row.Name+" is read-only.")
		},
	}
}

// allowWritesDialog asks before a project's writes are armed again. That takes a
// protection away, so it is confirmed the way a removal is: by typing the name.
func (p *projectsScreen) allowWritesDialog(row projectRow) dialog {
	// The command asks before re-arming writes, and a run has no terminal to ask
	// on. This dialog is that question, so its answer is the --yes.
	args := projectArgs("read-only", row.Name, "--off", "--yes")
	a := action{inv: Invocation{Args: args, Exclusive: true}, display: commandLine(args)}
	question := fmt.Sprintf("Allow writes to %s again?", row.Name)
	detail := "fft will send creates, updates and deletes to it again."
	return newTypeNameDialog(p.st, question, detail, row.Name, a.display, func() tea.Cmd {
		done := row.Name + " accepts writes again."
		if p.s.readOnlyFloor {
			// The project's own setting is off, but the session's floor is not, and a
			// notice that said only the first would promise writes that are refused.
			done = row.Name + " accepts writes again, but this session still refuses every write " +
				"(fft tui --read-only or FFT_READ_ONLY)."
		}
		return p.startReadOnly(a, row.Name, done)
	})
}

// startReadOnly runs a read-only change, and reloads the list either way.
func (p *projectsScreen) startReadOnly(a action, name, done string) tea.Cmd {
	return p.s.start(a, func(r Result) tea.Cmd {
		if r.ExitCode != exitcode.OK {
			p.fail("changing "+name, r)
		} else {
			p.succeed(done)
		}
		return p.reload()
	})
}

func (p *projectsScreen) removeDialog(row projectRow) dialog {
	args := projectArgs("remove", row.Name, "--yes")
	a := action{inv: Invocation{Args: args, Exclusive: true}, display: commandLine(args)}
	question := fmt.Sprintf("Remove %s and its stored credentials?", row.Name)
	return newTypeNameDialog(p.st, question, "", row.Name, a.display, func() tea.Cmd {
		return p.s.start(a, func(r Result) tea.Cmd {
			if r.ExitCode != exitcode.OK {
				p.fail("removing "+row.Name, r)
				return p.reload()
			}
			p.succeed("Removed " + row.Name + ".")
			switch row.Name {
			case p.s.project:
				// The UI's choice is gone with it; fft's own resolution decides again.
				// A session working against the emulator stays there: it is the project
				// it would have gone back to that was removed, not the emulator.
				p.s.forgetProject()
				p.syncCurrent()
			case p.s.currentProject():
				// fft's own choice is gone; until the list says what it chooses now,
				// nothing known about the removed project may pass for the current one.
				p.s.forgetCurrent()
			}
			return tea.Batch(p.reload(), p.refreshStatus(false))
		})
	})
}

func (p *projectsScreen) refreshToken() tea.Cmd {
	a := p.s.scoped("auth", "refresh")
	a.inv.Exclusive = true
	p.succeed("Refreshing the token…")
	return p.s.start(a, func(r Result) tea.Cmd {
		if r.ExitCode != exitcode.OK {
			p.fail("refreshing the token", r)
		} else {
			p.succeed("Signed in again.")
		}
		return p.refreshStatus(false)
	})
}

func (p *projectsScreen) submitForm() tea.Cmd {
	form := p.form
	a := form.action()
	name := form.value(rowName)
	form.submitting = true
	form.failure = nil
	// See use: the add can take a while, and the emulator row is reachable while
	// it is in flight.
	asked := p.s.selections
	return p.s.start(a, func(r Result) tea.Cmd {
		form.submitting = false
		if r.ExitCode != exitcode.OK {
			form.failure = &failure{what: "adding " + name, result: r}
			return nil
		}
		// Only the form that was submitted may be answered with a question. One the
		// user has since closed, or replaced with another, has left the screen, and a
		// question asked in its place would take the keys of whatever came next.
		stillOpen := p.form == form
		if stillOpen {
			p.form = nil
		}

		var added projectRow
		// The table's summary is enough if the document cannot be read: the command
		// has already succeeded, and the list is about to be reloaded.
		_ = json.Unmarshal(r.Stdout, &added)

		if added.Active && p.s.project == "" && p.s.selections == asked && !p.s.usingEmulator() {
			// fft made it the active project, and the UI follows fft's choice — unless
			// the user has since chosen something else, or is working against the
			// emulator, in which case following it now would silently move the session
			// onto a tenant. Adding a project is not a request to leave the emulator,
			// and a first run with nothing configured is exactly when both happen at
			// once; the switch is offered below instead of made here.
			p.succeed("Added " + name + "; it is now the active project.")
			p.selectProject(name)
			return tea.Batch(p.reload(), p.warmUp())
		}

		if name == p.s.currentProject() {
			// A --force add under the name in use: the UI stays on it, but it may now
			// sign in as someone else, whose roles and token are not the old ones.
			p.s.forgetCurrent()
			p.succeed("Replaced " + name + ".")
			return tea.Batch(p.reload(), p.warmUp())
		}

		if p.s.usingEmulator() {
			// Said plainly, because fft may well have made it active and the table is
			// about to mark it so while the session goes on reaching the emulator.
			p.succeed("Added " + name + ", but this session is still using the emulator.")
			if !stillOpen {
				return p.reload()
			}
			p.dialog = armed(&confirmDialog{
				question: fmt.Sprintf("Switch to %s now?", name),
				detail:   "This session stops using the emulator, which goes on running.",
				command:  p.useAction(name).display,
				onYes:    func() tea.Cmd { return p.use(name) },
			}, p.s.now, true)
			return p.reload()
		}

		if !stillOpen {
			p.succeed("Added " + name + ". Select it and press enter to switch to it.")
			return p.reload()
		}
		p.succeed("Added " + name + ".")
		// The form was still open, so the question is in front of the user now, and
		// may meet a key meant for the form.
		p.dialog = armed(&confirmDialog{
			question: fmt.Sprintf("Switch to %s now?", name),
			command:  p.useAction(name).display,
			onYes:    func() tea.Cmd { return p.use(name) },
		}, p.s.now, true)
		return p.reload()
	})
}

func (p *projectsScreen) bindings() []key.Binding {
	switch {
	case p.dialog != nil:
		return p.dialog.bindings()
	case p.form != nil:
		return p.form.bindings()
	}
	if p.emulator.open {
		return p.emulator.bindings()
	}
	k := p.keys
	if p.s.headless {
		// The changes are refused here, so the help does not offer them — but the
		// emulator row is still selectable, and enter is what selects it.
		return []key.Binding{k.up, k.use, k.refresh, k.reload, k.emulator}
	}
	return []key.Binding{k.up, k.use, k.readOnly, k.remove, k.refresh, k.add, k.reload, k.emulator}
}

func (p *projectsScreen) legend() []legendSection {
	k := p.keys
	main := legendSection{entries: []legendEntry{
		{of: k.up, desc: "select a project"},
		{of: k.use, keys: "enter, u", desc: "use it (fft project use), or the emulator row to work offline"},
		{of: k.readOnly, desc: "make it read-only, or allow writes again"},
		{of: k.remove, desc: "remove it and its stored credentials"},
		{of: k.refresh, desc: "sign in again now (fft auth refresh)"},
		{of: k.add, desc: "add a project"},
		{of: k.reload, desc: "read the list and the credentials again"},
		{of: k.emulator, desc: "run the local offline emulator"},
	}}
	if p.s.headless {
		main.note = "Running from the environment: enter, u, r, d and a change nothing here, " +
			"except on the emulator row, which is not in the config file."
		// The add form never opens here; the emulator pane still does.
		return []legendSection{main, p.emulator.legend()}
	}
	f := newFormKeys()
	return []legendSection{main, {
		title:   "In the add form",
		compact: true,
		entries: []legendEntry{
			{of: f.next, keys: "tab/↓/enter", desc: "next field"},
			{of: f.prev, keys: "shift+tab/↑", desc: "previous field"},
			{of: f.toggle, desc: "switch a toggle"},
			{of: f.submit, desc: "add the project (so does enter on the last field)"},
			{of: f.cancel, desc: "cancel"},
		},
	}, p.emulator.legend()}
}

func (p *projectsScreen) equivalent() shellCommand {
	switch {
	case p.dialog != nil:
		return p.dialog.equivalent()
	case p.form != nil:
		return commandLine(p.form.args())
	case p.emulator.open:
		return p.emulator.equivalent()
	}
	if row, ok := p.selected(); ok {
		if row.emulator {
			// There is no `fft project use emulator` to show — it is not in the config
			// file. What a shell does instead is run the emulator and export the recipe.
			return p.emulator.equivalent()
		}
		if !p.s.headless {
			return p.useAction(row.Name).display
		}
	}
	return commandLine([]string{"project", "list"})
}

func (p *projectsScreen) view(width, height int) string {
	st := p.st
	if p.dialog != nil {
		// A dialog has the keyboard, over the list and over the form alike, so it is
		// drawn in their place: first, where no height can cut it off.
		return strings.Join([]string{st.title.Render("Projects"), "", p.dialog.view(st, width, height-2)}, "\n")
	}
	if p.form != nil {
		return p.form.view(width)
	}
	if p.emulator.open {
		return p.emulator.view(width, height)
	}

	lines := []string{st.title.Render("Projects")}
	if p.s.headless {
		lines = append(lines, st.warnText.Render("Running from the environment: projects are read-only here."))
	}
	if p.s.usingEmulator() && !p.emulator.running() {
		lines = append(lines, st.warnText.Render(
			"Using the emulator, which is not running: requests fail until it is started (e, then s)."))
	}
	lines = append(lines, "")

	switch {
	case !p.loaded && p.failure == nil:
		lines = append(lines, st.dim.Render("Loading projects…"))
	default:
		// Said above the table rather than instead of it: once the list has been read,
		// the emulator row is still there, and it is the one thing a first run can
		// usefully press enter on. Only once it has been read — a list that could not
		// be read says nothing about what is configured, and the failure below says
		// what actually happened.
		if p.loaded && p.listed == 0 {
			lines = append(lines, "No projects are configured. Press a to add one.", "")
		}
		lines = append(lines, p.table(width)...)
	}

	lines = append(lines, "")
	if p.notice != "" {
		lines = append(lines, wrap(st.okText.Render(output.SanitizeCell(p.notice)), width))
	}
	if p.refused {
		lines = append(lines, wrap(st.warnText.Render("Nothing was sent: "+headlessExplanation), width))
	}
	if p.refusedEmulator {
		lines = append(lines, wrap(st.warnText.Render("Nothing was sent: "+emulatorExplanation), width))
	}
	if p.failure != nil {
		lines = append(lines, p.failure.view(st, width))
	}
	return strings.Join(lines, "\n")
}

// projectColumns ends in an unnamed one, which only the emulator row fills: it is
// not a configured project, and a table that did not say so would be offering a
// row that r and d refuse.
var projectColumns = []string{"NAME", "BASE URL", "EMAIL", "CREDENTIAL", "ACCESS", ""}

func (p *projectsScreen) table(width int) []string {
	current := p.s.currentProject()
	cells := make([][]string, 0, len(p.rows)+1)
	cells = append(cells, projectColumns)
	for _, r := range p.rows {
		name := "  " + r.Name
		if r.emulator && p.s.usingEmulator() || !r.emulator && r.Name == current {
			name = "* " + r.Name
		}
		access := "writable"
		if r.ReadOnly || p.s.readOnlyFloor {
			access = "read-only"
		}
		mark := ""
		if r.emulator {
			mark = "(emulator)"
		}
		cells = append(cells, []string{
			output.SanitizeCell(name),
			output.SanitizeCell(r.BaseURL),
			output.SanitizeCell(r.Email),
			output.SanitizeCell(r.Credential),
			access,
			mark,
		})
	}

	widths := make([]int, len(projectColumns))
	for _, row := range cells {
		for i, c := range row {
			widths[i] = max(widths[i], ansi.StringWidth(c))
		}
	}

	lines := make([]string, 0, len(cells))
	for i, row := range cells {
		var b strings.Builder
		for j, c := range row {
			b.WriteString(c)
			if j < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-ansi.StringWidth(c)+2))
			}
		}
		marker := "  "
		// Trimmed because the last column is empty on every row but the emulator's,
		// and a header line padded out to a column nothing in it fills is trailing
		// whitespace on every screen.
		line := strings.TrimRight(b.String(), " ")
		switch {
		case i == 0:
			line = p.st.dim.Render(marker + line)
		case i-1 == p.cursor:
			line = p.st.selected.Render("> " + line)
		default:
			line = marker + line
		}
		lines = append(lines, clip(line, width))
	}
	return lines
}

// wrap breaks s to width cells, and leaves it alone while the width is unknown.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Wrap(s, width, "")
}
