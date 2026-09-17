package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// screenNames are the screens in tab order; the number key for each is its
// position, counting from one.
var screenNames = []string{"Projects", "Operations", "Request", "Response", "Templates", "History", "Roles"}

// The screens' positions in screenNames.
const (
	tabProjects = iota
	tabOperations
	tabRequest
	tabResponse
	tabTemplates
	tabHistory
	tabRoles
)

// screen is one tab of the UI.
type screen interface {
	// update handles a key, or text pasted while the screen is focused.
	update(msg tea.Msg) tea.Cmd
	view(width, height int) string
	bindings() []key.Binding

	// legend is every key the screen takes, in each of its modes, with what it
	// does: the hint line shows the keys that work right now, the legend all of them.
	legend() []legendSection

	// equivalent is the fft command for what the screen's focused action would do.
	equivalent() shellCommand

	// focused reports whether a dialog or a text field has the keyboard, in which
	// case the global keys, but for ctrl+c, are the screen's to interpret.
	focused() bool
}

// receiver is a screen that takes messages other than keys: the results of work it
// started in the background. Every receiver is offered every such message, and
// ignores what is not its own.
type receiver interface {
	receive(msg tea.Msg) tea.Cmd
}

// app is the root model: the tab bar, the screen underneath it, the in-flight
// panel, the status bar and the hint line.
type app struct {
	s          *session
	st         styles
	keys       globalKeys
	legendKeys legendKeys
	events     <-chan RunEvent

	spin     spinner.Model
	spinning bool

	width, height int

	current    int
	screens    []screen
	projects   *projectsScreen
	operations *operationsScreen
	request    *requestScreen
	response   *responseScreen
	templates  *templatesScreen
	history    *historyScreen
	roles      *rolesScreen

	panel     *runsPanel
	showPanel bool

	confirmQuit bool

	// legendOpen is set while the key legend is drawn in place of the screen, and
	// legendScroll is how far down it is scrolled.
	legendOpen   bool
	legendScroll int

	// flash is a one-keystroke message in the status bar, such as what y copied.
	flash string
}

func newApp(opts Options) *app {
	st := newStyles(opts.Color)
	s := newSession(opts, st)

	m := &app{
		s:          s,
		st:         st,
		keys:       newGlobalKeys(),
		legendKeys: newLegendKeys(),
		events:     opts.Runner.Events(),
		spin:       spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		panel:      newRunsPanel(s),
	}
	m.projects = newProjectsScreen(s, st)
	m.operations = newOperationsScreen(s, st, m, opts.Catalog)
	m.request = newRequestScreen(s, st, m)
	m.response = newResponseScreen(s, st, opts.Catalog, m)
	m.templates = newTemplatesScreen(s, st, m, opts.Catalog)
	m.history = newHistoryScreen(s, st, m, opts.Catalog, opts.History)
	m.roles = newRolesScreen(s, st, opts.Catalog)
	m.operations.hint = m.hint
	m.screens = []screen{
		m.projects,
		m.operations,
		m.request,
		m.response,
		m.templates,
		m.history,
		m.roles,
	}
	return m
}

// hint is what the Operations list shows beside op: how often it was sent to the
// current project, and what the user appears to lack for it.
func (m *app) hint(op Operation) opHint {
	return opHint{uses: m.history.usesOf(op.ID), lacking: m.s.lacking(op)}
}

var _ navigator = (*app)(nil)

func (m *app) openOperations() tea.Cmd {
	m.current = tabOperations
	return nil
}

func (m *app) openRequest(op Operation) tea.Cmd {
	m.request.open(op)
	m.current = tabRequest
	return nil
}

func (m *app) openRequestScreen() tea.Cmd {
	m.current = tabRequest
	return nil
}

func (m *app) sendBody(op Operation, body []byte, from string) tea.Cmd {
	m.current = tabRequest
	return m.request.sendBody(op, body, from)
}

func (m *app) unsentForm(body []byte) (form, lost string) {
	return m.request.unsent(body)
}

func (m *app) openRecalled(op Operation, rc recalled) tea.Cmd {
	m.request.openRecalled(op, rc)
	m.current = tabRequest
	return nil
}

func (m *app) templatesChanged() {
	m.templates.changed()
}

func (m *app) openResponse(id RunID) {
	m.response.show(id)
	m.current = tabResponse
	// What was asked for is drawn, and what is drawn has the keyboard.
	m.showPanel = false
}

func (m *app) showing(scr screen) bool {
	return m.screens[m.current] == scr && !m.showPanel && !m.confirmQuit && !m.legendOpen
}

func (m *app) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.events), m.projects.init(), tea.RequestBackgroundColor)
}

func (m *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case runEventMsg:
		if msg.State == RunDone && msg.Result.Recorded {
			// Recorded before the runner says it is done.
			m.history.runFinished()
		}
		m.response.observe(RunEvent(msg))
		cmds = append(cmds, m.s.handle(RunEvent(msg)), waitForEvent(m.events))
	case tea.BackgroundColorMsg:
		m.st.hint = newHintStyles(m.st.color, msg.IsDark())
	case runnerClosedMsg:
		// The runner is shut down only as the UI goes; there is nothing left to wait for.
	case spinner.TickMsg:
		if m.s.runs.inFlight() == 0 {
			// Dropping the tick stops the spinner; the next run starts it again.
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		cmds = append(cmds, cmd)
	case tea.KeyPressMsg:
		cmds = append(cmds, m.key(msg))
	case tea.PasteMsg:
		// A paste is typing, so it goes where typing goes: to a focused field. With
		// nothing focused, pasted text would be read as a burst of commands.
		switch m.owner() {
		case ownerFocused:
			cmds = append(cmds, m.screens[m.current].update(msg))
		case ownerQuestion:
			cmds = append(cmds, m.s.answerWith(msg))
		}
	default:
		for _, scr := range m.screens {
			if r, ok := scr.(receiver); ok {
				cmds = append(cmds, r.receive(msg))
			}
		}
	}

	if m.s.runs.inFlight() > 0 && !m.spinning {
		m.spinning = true
		cmds = append(cmds, m.spin.Tick)
	}
	// A question is armed from the moment it is what the user sees, which may be
	// long after it arrived.
	if m.owner() == ownerQuestion {
		m.s.asking().dialog.arm()
	}
	m.operations.resize(m.width, m.bodyHeight(m.hintLine()))
	cmds = append(cmds, m.readShown())
	m.history.sync()
	return m, tea.Batch(cmds...)
}

// readShown reads what the screen on display shows and is not current: the
// templates, the history, the user's roles. Each is read when somebody looks, not
// when the UI starts, and read again once it may have changed — after a run for
// the history, after a project switch for the roles.
func (m *app) readShown() tea.Cmd {
	switch m.screens[m.current] {
	case m.templates:
		return m.templates.shown()
	case m.operations:
		return tea.Batch(m.roles.want(), m.history.want())
	case m.request:
		return m.roles.want()
	case m.history:
		return m.history.want()
	case m.roles:
		return m.roles.want()
	}
	return nil
}

// bodyHeight is how many rows the screen gets under the tabs and above the status
// bar and hint, which may take several rows: none on a terminal too small for
// more than those.
func (m *app) bodyHeight(hint string) int {
	return max(m.height-3-lipgloss.Height(hint), 0)
}

// ruleMinBody is the fewest body rows the rule above the hint is drawn beside. The
// rule is only a divider, and a short terminal needs the row more.
const ruleMinBody = 8

// hintLine is the keys that work right now, on as many rows as they need, under a
// rule that sets them apart from the status bar.
func (m *app) hintLine() string {
	hint := hintView(m.st, m.width, m.bindings())
	if m.width <= 0 || m.height-3-lipgloss.Height(hint)-1 < ruleMinBody {
		return hint
	}
	return m.st.hint.rule.Render(strings.Repeat("─", m.width)) + "\n" + hint
}

// keyOwner is the part of the UI the next key goes to.
type keyOwner int

const (
	// ownerScreen is the screen, under the global keys.
	ownerScreen keyOwner = iota
	// ownerPanel is the command panel, under the global keys.
	ownerPanel
	// ownerFocused is a dialog or a form on the screen: its own keys, and ctrl+c.
	ownerFocused
	// ownerQuestion is a question a running command asks: its own keys, and ctrl+c.
	ownerQuestion
	// ownerLegend is the key legend: its own keys, and quit.
	ownerLegend
	// ownerQuit is the question whether to quit.
	ownerQuit
)

// owner decides who has the keyboard. The key handling, the body, the hint line
// and the status bar all ask it, so that whatever takes the next key is what is
// drawn: a question the user cannot see must never be one a keystroke answers.
//
// A command's question comes when the command gets to it, not when the user asks
// for it, so it waits behind whatever the user is in the middle of: a field being
// typed into, a dialog, the open command panel, where the run shows as waiting.
// Whatever screen is underneath, it is asked as soon as none of those is open.
func (m *app) owner() keyOwner {
	switch {
	case m.confirmQuit:
		return ownerQuit
	case m.legendOpen:
		return ownerLegend
	case m.screens[m.current].focused():
		return ownerFocused
	case m.showPanel:
		return ownerPanel
	case m.s.asking() != nil:
		return ownerQuestion
	default:
		return ownerScreen
	}
}

func (m *app) key(msg tea.KeyPressMsg) tea.Cmd {
	m.flash = ""
	scr := m.screens[m.current]

	switch m.owner() {
	case ownerQuit:
		switch {
		case key.Matches(msg, yesKey), key.Matches(msg, m.keys.forceQ):
			return tea.Quit
		case key.Matches(msg, noKey):
			m.confirmQuit = false
		}
		return nil
	case ownerFocused:
		if key.Matches(msg, m.keys.forceQ) {
			return m.quit()
		}
		return scr.update(msg)
	case ownerQuestion:
		if key.Matches(msg, m.keys.forceQ) {
			return m.quit()
		}
		return m.s.answerWith(msg)
	case ownerLegend:
		return m.legendKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.quit):
		return m.quit()
	case key.Matches(msg, m.keys.help):
		m.legendOpen, m.legendScroll = true, 0
	case key.Matches(msg, m.keys.runs):
		m.showPanel = !m.showPanel
	case key.Matches(msg, m.keys.copy):
		return m.copyEquivalent()
	case key.Matches(msg, m.keys.next):
		m.current = (m.current + 1) % len(m.screens)
	case key.Matches(msg, m.keys.prev):
		m.current = (m.current + len(m.screens) - 1) % len(m.screens)
	case key.Matches(msg, m.keys.projects):
		// Projects is where a project is switched; a picker of its own can come later.
		m.current = tabProjects
		m.showPanel = false
	default:
		for i, b := range m.keys.screens {
			if key.Matches(msg, b) {
				m.current = i
				return nil
			}
		}
		// An open panel has the keyboard, so that its c cancels a run rather than
		// meaning whatever c means on the screen underneath.
		if m.showPanel {
			switch m.panel.update(msg) {
			case panelClose:
				m.showPanel = false
			case panelOpen:
				m.openResponse(m.panel.selected().id)
			}
			return nil
		}
		return scr.update(msg)
	}
	return nil
}

// legendKey handles a key while the legend is open. It swallows every key it does
// not know: the legend covers the screen, and a key must not act on what the user
// cannot see.
func (m *app) legendKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.legendKeys
	switch {
	case key.Matches(msg, k.close):
		m.legendOpen = false
	case key.Matches(msg, m.keys.quit):
		return m.quit()
	case key.Matches(msg, k.up):
		m.scrollLegend(-1)
	case key.Matches(msg, k.down):
		m.scrollLegend(1)
	case key.Matches(msg, k.pageUp):
		m.scrollLegend(-max(m.legendHeight()-2, 1))
	case key.Matches(msg, k.pageDn):
		m.scrollLegend(max(m.legendHeight()-2, 1))
	}
	return nil
}

// quit leaves at once when nothing is running, and asks first when something is:
// leaving cancels it, and a write that is cancelled may or may not have landed.
func (m *app) quit() tea.Cmd {
	if m.s.runs.inFlight() == 0 {
		return tea.Quit
	}
	m.confirmQuit = true
	return nil
}

// equivalent is the command the part of the UI with the keyboard stands for. A
// focused dialog or form shows its own, and the quit question and the legend stand
// for none: while they are open, y copies nothing.
func (m *app) equivalent() shellCommand {
	switch m.owner() {
	case ownerPanel:
		return m.panel.equivalent()
	case ownerScreen:
		return m.screens[m.current].equivalent()
	default:
		return shellCommand{}
	}
}

// copyEquivalent puts the focused action's fft command on the clipboard, through
// the terminal (OSC 52), so that it works over SSH too.
func (m *app) copyEquivalent() tea.Cmd {
	eq := m.equivalent()
	switch {
	case eq.empty():
		m.flash = "Nothing to copy here."
		return nil
	case eq.unportable:
		m.flash = "This command cannot be copied safely: a value holds a quote, a backslash or a " +
			"control character that shells do not all read the same way."
		return nil
	}
	m.flash = "Copied: " + eq.line
	return tea.SetClipboard(eq.line)
}

// bindings is what the hint line offers. While a dialog, a form or the quit
// question has the keyboard, the global keys do nothing, so only its own keys are
// shown: a hint that offered y to copy under a dialog whose y means yes would be
// advertising the wrong one. For the same reason ? is offered only where it opens
// the legend, and inside the legend as what closes it.
func (m *app) bindings() helpKeys {
	switch m.owner() {
	case ownerQuit:
		return helpKeys{local: []key.Binding{yesKey, noKey}}
	case ownerFocused:
		return helpKeys{local: m.screens[m.current].bindings()}
	case ownerQuestion:
		return helpKeys{local: m.s.asking().dialog.bindings()}
	case ownerLegend:
		return helpKeys{local: []key.Binding{m.legendKeys.up}, global: []key.Binding{m.keys.quit, m.legendKeys.close}}
	case ownerPanel:
		return helpKeys{local: m.panel.bindings(), global: m.keys.bindings()}
	default:
		return helpKeys{local: m.screens[m.current].bindings(), global: m.keys.bindings()}
	}
}

func (m *app) View() tea.View {
	tabs := m.tabBar()
	status := m.statusBar()
	hint := m.hintLine()

	// Tabs, a blank line, the body, the status bar and the hint. On a terminal too
	// small for all of it the body gets nothing, and the frame is cut to the height
	// below, so that nothing is ever drawn past the last row.
	bodyHeight := m.bodyHeight(hint)

	owner := m.owner()
	var body string
	switch owner {
	case ownerQuit:
		body = m.quitDialog()
	case ownerQuestion:
		body = m.s.asking().dialog.view(m.st, m.width, bodyHeight)
	case ownerLegend:
		body = m.legendView(m.legendHeight())
	default:
		body = m.screens[m.current].view(m.width, bodyHeight)
	}

	// The panel is drawn only while it has the keyboard: under a focused dialog it
	// would take half the body, and could cut off the very question being asked.
	if owner == ownerPanel {
		body = m.withPanel(body, bodyHeight)
	}

	var content string
	switch {
	case m.height > 0 && owner == ownerLegend && m.legendHeight() > bodyHeight:
		// Too short for the legend beside the chrome: it takes the terminal, and its
		// title says how to close it.
		content = fit(body, m.height)
	case m.height > 0 && owner >= ownerFocused && lipgloss.Height(body) > bodyHeight:
		// A question that does not fit beside the chrome gets the whole terminal:
		// the tabs and the help can go, the question the next key answers cannot.
		content = fit(body, m.height)
	case m.height > 0:
		parts := []string{tabs, ""}
		if bodyHeight > 0 {
			parts = append(parts, fit(body, bodyHeight))
		}
		content = fit(strings.Join(append(parts, status, hint), "\n"), m.height)
	default:
		content = strings.Join([]string{tabs, "", body, status, hint}, "\n")
	}

	v := tea.NewView(content)
	// The alternate screen gives the shell's scrollback back untouched on exit.
	v.AltScreen = true
	v.WindowTitle = "fft"
	return v
}

// withPanel puts the command panel under body, both within bodyHeight rows. The
// panel keeps its rows first: it is what has the keyboard.
func (m *app) withPanel(body string, bodyHeight int) string {
	if m.height <= 0 {
		return body + "\n" + m.panel.view(m.st, m.spin.View(), m.width, 5)
	}
	panelHeight := min(max(bodyHeight/2, 5), bodyHeight)
	panel := fit(m.panel.view(m.st, m.spin.View(), m.width, panelHeight), panelHeight)
	above := bodyHeight - panelHeight
	if above <= 0 {
		return panel
	}
	return fit(body, above) + "\n" + panel
}

func (m *app) tabBar() string {
	tabs := make([]string, len(screenNames))
	for i, name := range screenNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if i == m.current {
			tabs[i] = m.st.activeTab.Render("[" + label + "]")
		} else {
			tabs[i] = m.st.tab.Render(" " + label + " ")
		}
	}
	return clip(lipgloss.JoinHorizontal(lipgloss.Top, tabs...), m.width)
}

func (m *app) statusBar() string {
	project := m.s.currentProject()
	switch {
	case project == "":
		project = "no project"
	case m.s.headless && m.s.project == "":
		project += " (environment)"
	}
	parts := []string{"fft", output.SanitizeCell(project)}
	if m.s.readOnly() {
		parts = append(parts, m.st.badge.Render("RO"))
	}
	// The token's state is read from a command's JSON, like any other response.
	parts = append(parts, output.SanitizeCell(m.s.status.tokenSummary(m.s.now())))
	if n := m.s.runs.inFlight(); n > 0 {
		parts = append(parts, fmt.Sprintf("%s %d running", m.spin.View(), n))
	}
	if n := len(m.s.questions); n > 0 {
		parts = append(parts, m.st.warnText.Render(fmt.Sprintf("%d waiting for your answer", n)))
	}
	left := " " + strings.Join(parts, " · ")

	right := m.flash
	if right == "" {
		if eq := m.equivalent(); !eq.empty() {
			right = "$ " + eq.String()
		}
	}
	line := left
	if right != "" {
		line += "   " + output.SanitizeCell(right)
	}
	line = clip(line, m.width)
	if m.width > 0 {
		return m.st.statusBar.Width(m.width).Render(line)
	}
	return m.st.statusBar.Render(line)
}

func (m *app) quitDialog() string {
	n := m.s.runs.inFlight()
	noun := "commands are"
	if n == 1 {
		noun = "command is"
	}
	d := &confirmDialog{
		question: fmt.Sprintf("%d %s still running. Quit and cancel them?", n, noun),
		detail:   "A write that is cancelled may already have reached the tenant.",
	}
	return d.view(m.st, m.width, 0)
}

// fit makes s exactly height lines tall: cut when it is longer, padded when it is
// shorter, so that what comes after it stays where it is.
func fit(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:max(height, 0)]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
