package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// headlessExplanation is why the Projects screen changes nothing in headless mode.
// It says what `project add` and its siblings would say, before sending them.
const headlessExplanation = "fft is running from the environment (FFT_BASE_URL is set), so projects " +
	"cannot be added, switched, changed or removed here. Unset the FFT_* variables to manage the config file."

// projectRow is one entry of `fft project list -o json`.
type projectRow struct {
	Name       string `json:"name"`
	Active     bool   `json:"active"`
	BaseURL    string `json:"baseUrl"`
	Email      string `json:"email"`
	Credential string `json:"credential"`
	ReadOnly   bool   `json:"readOnly"`
	Ephemeral  bool   `json:"ephemeral"`
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
	}
}

// projectsScreen lists the configured projects and manages them, each action
// through the fft command that does it.
type projectsScreen struct {
	s    *session
	st   styles
	keys projectKeys

	rows    []projectRow
	cursor  int
	loaded  bool
	warmed  bool
	notice  string
	refused bool
	failure *failure

	dialog dialog
	form   *addForm
}

func newProjectsScreen(s *session, st styles) *projectsScreen {
	return &projectsScreen{s: s, st: st, keys: newProjectKeys()}
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
	row, ok := p.selected()
	switch {
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
		if p.refuseHeadless() {
			return nil
		}
		return p.use(row.Name)
	case key.Matches(msg, p.keys.readOnly):
		if p.refuseHeadless() {
			return nil
		}
		p.dialog = p.readOnlyDialog(row)
	case key.Matches(msg, p.keys.remove):
		if p.refuseHeadless() {
			return nil
		}
		p.dialog = p.removeDialog(row)
	}
	return nil
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
	p.refused = false
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
	p.rows, p.loaded = rows, true

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
		if r.Name == current {
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

func (p *projectsScreen) useAction(name string) action {
	args := []string{"project", "use", name}
	return action{inv: Invocation{Args: args, Exclusive: true}, display: commandLine(args)}
}

// use makes name the active project and the one the UI acts on, then signs in to
// it before anything else can run.
func (p *projectsScreen) use(name string) tea.Cmd {
	p.succeed("Switching to " + name + "…")
	return p.s.start(p.useAction(name), func(r Result) tea.Cmd {
		if r.ExitCode != exitcode.OK {
			// A refused switch changed nothing, so there is nothing to reload.
			p.fail("switching to "+name, r)
			return nil
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
	args := []string{"project", "read-only", row.Name}
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
	args := []string{"project", "read-only", row.Name, "--off", "--yes"}
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
	args := []string{"project", "remove", row.Name, "--yes"}
	a := action{inv: Invocation{Args: args, Exclusive: true}, display: commandLine(args)}
	question := fmt.Sprintf("Remove %s and its stored credentials?", row.Name)
	return newTypeNameDialog(p.st, question, "", row.Name, a.display, func() tea.Cmd {
		return p.s.start(a, func(r Result) tea.Cmd {
			if r.ExitCode != exitcode.OK {
				p.fail("removing "+row.Name, r)
				return p.reload()
			}
			p.succeed("Removed " + row.Name + ".")
			if p.s.project == row.Name {
				// The UI's choice is gone with it; fft's own resolution decides again.
				p.selectProject("")
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

		if added.Active && p.s.project == "" {
			// fft made it the active project, and the UI follows fft's choice.
			p.succeed("Added " + name + "; it is now the active project.")
			p.selectProject(name)
			return tea.Batch(p.reload(), p.warmUp())
		}

		if !stillOpen {
			p.succeed("Added " + name + ". Select it and press enter to switch to it.")
			return p.reload()
		}
		p.succeed("Added " + name + ".")
		p.dialog = &confirmDialog{
			question: fmt.Sprintf("Switch to %s now?", name),
			command:  p.useAction(name).display,
			onYes:    func() tea.Cmd { return p.use(name) },
		}
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
	k := p.keys
	if p.s.headless {
		// The changes are refused here, so the help does not offer them.
		return []key.Binding{k.up, k.refresh, k.reload}
	}
	return []key.Binding{k.up, k.use, k.readOnly, k.remove, k.refresh, k.add, k.reload}
}

func (p *projectsScreen) equivalent() shellCommand {
	switch {
	case p.dialog != nil:
		return p.dialog.equivalent()
	case p.form != nil:
		return commandLine(p.form.args())
	}
	if row, ok := p.selected(); ok && !p.s.headless {
		return p.useAction(row.Name).display
	}
	return commandLine([]string{"project", "list"})
}

func (p *projectsScreen) view(width, _ int) string {
	st := p.st
	if p.dialog != nil {
		// A dialog has the keyboard, over the list and over the form alike, so it is
		// drawn in their place: first, where no height can cut it off.
		return strings.Join([]string{st.title.Render("Projects"), "", p.dialog.view(st, width)}, "\n")
	}
	if p.form != nil {
		return p.form.view(width)
	}

	lines := []string{st.title.Render("Projects")}
	if p.s.headless {
		lines = append(lines, st.warnText.Render("Running from the environment: projects are read-only here."))
	}
	lines = append(lines, "")

	switch {
	case !p.loaded && p.failure == nil:
		lines = append(lines, st.dim.Render("Loading projects…"))
	case p.loaded && len(p.rows) == 0:
		lines = append(lines, "No projects are configured. Press a to add one.")
	default:
		lines = append(lines, p.table(width)...)
	}

	lines = append(lines, "")
	if p.notice != "" {
		lines = append(lines, wrap(st.okText.Render(output.SanitizeCell(p.notice)), width))
	}
	if p.refused {
		lines = append(lines, wrap(st.warnText.Render("Nothing was sent: "+headlessExplanation), width))
	}
	if p.failure != nil {
		lines = append(lines, p.failure.view(st, width))
	}
	return strings.Join(lines, "\n")
}

var projectColumns = []string{"NAME", "BASE URL", "EMAIL", "CREDENTIAL", "ACCESS"}

func (p *projectsScreen) table(width int) []string {
	current := p.s.currentProject()
	cells := make([][]string, 0, len(p.rows)+1)
	cells = append(cells, projectColumns)
	for _, r := range p.rows {
		name := "  " + r.Name
		if r.Name == current {
			name = "* " + r.Name
		}
		access := "writable"
		if r.ReadOnly || p.s.readOnlyFloor {
			access = "read-only"
		}
		cells = append(cells, []string{
			output.SanitizeCell(name),
			output.SanitizeCell(r.BaseURL),
			output.SanitizeCell(r.Email),
			output.SanitizeCell(r.Credential),
			access,
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
		line := b.String()
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
