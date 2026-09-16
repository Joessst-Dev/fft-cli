package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// screenNames are the screens in tab order; the number key for each is its
// position, counting from one.
var screenNames = []string{"Projects", "Operations", "Request", "Response", "Templates", "History", "Roles"}

// screen is one tab of the UI.
type screen interface {
	// update handles a key, or text pasted while the screen is focused.
	update(msg tea.Msg) tea.Cmd
	view(width, height int) string
	bindings() []key.Binding

	// equivalent is the fft command for what the screen's focused action would do.
	equivalent() string

	// focused reports whether a dialog or a text field has the keyboard, in which
	// case the global keys, but for ctrl+c, are the screen's to interpret.
	focused() bool
}

// comingSoon stands in for a screen a later release fills in.
type comingSoon struct {
	name string
	what string
}

func (c comingSoon) update(tea.Msg) tea.Cmd  { return nil }
func (c comingSoon) bindings() []key.Binding { return nil }
func (c comingSoon) equivalent() string      { return "" }
func (c comingSoon) focused() bool           { return false }

func (c comingSoon) view(int, int) string {
	return c.name + "\n\nComing soon: " + c.what + "."
}

// app is the root model: the tab bar, the screen underneath it, the in-flight
// panel, the status bar and the help line.
type app struct {
	s      *session
	st     styles
	keys   globalKeys
	help   help.Model
	events <-chan RunEvent

	spin     spinner.Model
	spinning bool

	width, height int

	current  int
	screens  []screen
	projects *projectsScreen

	panel     *runsPanel
	showPanel bool

	confirmQuit bool

	// flash is a one-keystroke message in the status bar, such as what y copied.
	flash string
}

func newApp(opts Options) *app {
	s := newSession(opts)
	st := newStyles(opts.Color)

	h := help.New()
	h.Styles = st.help

	projects := newProjectsScreen(s, st)
	return &app{
		s:      s,
		st:     st,
		keys:   newGlobalKeys(),
		help:   h,
		events: opts.Runner.Events(),
		spin:   spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		screens: []screen{
			projects,
			comingSoon{"Operations", "every operation, grouped by tag, with fuzzy search"},
			comingSoon{"Request", "a form for any operation, built from its flags"},
			comingSoon{"Response", "the last response as JSON, as a table, and its stderr"},
			comingSoon{"Templates", "saved request bodies, rendered and sent"},
			comingSoon{"History", "recent and most used requests"},
			comingSoon{"Roles", "your roles and what they permit"},
		},
		projects: projects,
		panel:    newRunsPanel(s),
	}
}

func (m *app) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.events), m.projects.init())
}

func (m *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
	case runEventMsg:
		cmds = append(cmds, m.s.handle(RunEvent(msg)), waitForEvent(m.events))
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
		if m.owner() == ownerFocused {
			cmds = append(cmds, m.screens[m.current].update(msg))
		}
	}

	if m.s.runs.inFlight() > 0 && !m.spinning {
		m.spinning = true
		cmds = append(cmds, m.spin.Tick)
	}
	return m, tea.Batch(cmds...)
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
	// ownerQuit is the question whether to quit.
	ownerQuit
)

// owner decides who has the keyboard. The key handling, the body, the help line
// and the status bar all ask it, so that whatever takes the next key is what is
// drawn: a question the user cannot see must never be one a keystroke answers.
func (m *app) owner() keyOwner {
	switch {
	case m.confirmQuit:
		return ownerQuit
	case m.screens[m.current].focused():
		return ownerFocused
	case m.showPanel:
		return ownerPanel
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
	}

	switch {
	case key.Matches(msg, m.keys.quit):
		return m.quit()
	case key.Matches(msg, m.keys.help):
		m.help.ShowAll = !m.help.ShowAll
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
		m.current = 0
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
			if m.panel.update(msg) {
				m.showPanel = false
			}
			return nil
		}
		return scr.update(msg)
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
// focused dialog or form shows its own, and the quit question stands for none.
func (m *app) equivalent() string {
	switch m.owner() {
	case ownerPanel:
		return m.panel.equivalent()
	case ownerScreen:
		return m.screens[m.current].equivalent()
	default:
		return ""
	}
}

// copyEquivalent puts the focused action's fft command on the clipboard, through
// the terminal (OSC 52), so that it works over SSH too.
func (m *app) copyEquivalent() tea.Cmd {
	eq := m.equivalent()
	if eq == "" {
		m.flash = "Nothing to copy here."
		return nil
	}
	m.flash = "Copied: " + eq
	return tea.SetClipboard(eq)
}

// bindings is what the help line offers. While a dialog, a form or the quit
// question has the keyboard, the global keys do nothing, so only its own keys are
// shown: a help line that offered y to copy under a dialog whose y means yes would
// be advertising the wrong one.
func (m *app) bindings() helpKeys {
	switch m.owner() {
	case ownerQuit:
		return helpKeys{local: []key.Binding{yesKey, noKey}}
	case ownerFocused:
		return helpKeys{local: m.screens[m.current].bindings()}
	case ownerPanel:
		return helpKeys{local: m.panel.bindings(), global: m.keys.bindings()}
	default:
		return helpKeys{local: m.screens[m.current].bindings(), global: m.keys.bindings()}
	}
}

func (m *app) View() tea.View {
	tabs := m.tabBar()
	status := m.statusBar()
	helpLine := m.help.View(m.bindings())

	// Tabs, a blank line, the body, the status bar and the help. On a terminal too
	// small for all of it the body gets nothing, and the frame is cut to the height
	// below, so that nothing is ever drawn past the last row.
	chrome := 3 + lipgloss.Height(helpLine)
	bodyHeight := max(m.height-chrome, 0)

	owner := m.owner()
	var body string
	switch owner {
	case ownerQuit:
		body = m.quitDialog()
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
	case m.height > 0 && owner >= ownerFocused && lipgloss.Height(body) > bodyHeight:
		// A question that does not fit beside the chrome gets the whole terminal:
		// the tabs and the help can go, the question the next key answers cannot.
		content = fitExactly(body, m.height)
	case m.height > 0:
		parts := []string{tabs, ""}
		if bodyHeight > 0 {
			parts = append(parts, fitExactly(body, bodyHeight))
		}
		content = fitExactly(strings.Join(append(parts, status, helpLine), "\n"), m.height)
	default:
		content = strings.Join([]string{tabs, "", body, status, helpLine}, "\n")
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
	panel := fitExactly(m.panel.view(m.st, m.spin.View(), m.width, panelHeight), panelHeight)
	above := bodyHeight - panelHeight
	if above <= 0 {
		return panel
	}
	return fitExactly(body, above) + "\n" + panel
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
	parts = append(parts, m.s.status.tokenSummary(m.s.now()))
	if n := m.s.runs.inFlight(); n > 0 {
		parts = append(parts, fmt.Sprintf("%s %d running", m.spin.View(), n))
	}
	left := " " + strings.Join(parts, " · ")

	right := m.flash
	if right == "" {
		if eq := m.equivalent(); eq != "" {
			right = "$ " + eq
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
	return d.view(m.st, m.width)
}

// fit makes s exactly height lines tall: cut when it is longer, padded when it is
// shorter, so that what comes after it stays where it is. A height of zero or less
// means the height is not known yet, and s is left alone.
func fit(s string, height int) string {
	if height <= 0 {
		return s
	}
	return fitExactly(s, height)
}

// fitExactly is fit for a height that is known, however small.
func fitExactly(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:max(height, 0)]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
