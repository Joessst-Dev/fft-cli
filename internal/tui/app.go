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
	update(msg tea.KeyPressMsg) tea.Cmd
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

func (c comingSoon) update(tea.KeyPressMsg) tea.Cmd { return nil }
func (c comingSoon) bindings() []key.Binding        { return nil }
func (c comingSoon) equivalent() string             { return "" }
func (c comingSoon) focused() bool                  { return false }

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
	}

	if m.s.runs.inFlight() > 0 && !m.spinning {
		m.spinning = true
		cmds = append(cmds, m.spin.Tick)
	}
	return m, tea.Batch(cmds...)
}

func (m *app) key(msg tea.KeyPressMsg) tea.Cmd {
	m.flash = ""

	if m.confirmQuit {
		switch {
		case key.Matches(msg, yesKey), key.Matches(msg, m.keys.forceQ):
			return tea.Quit
		case key.Matches(msg, noKey):
			m.confirmQuit = false
		}
		return nil
	}

	scr := m.screens[m.current]
	if key.Matches(msg, m.keys.forceQ) {
		return m.quit()
	}
	if scr.focused() {
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

func (m *app) equivalent() string {
	if m.showPanel {
		return m.panel.equivalent()
	}
	return m.screens[m.current].equivalent()
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

func (m *app) bindings() helpKeys {
	local := m.screens[m.current].bindings()
	if m.showPanel {
		local = m.panel.bindings()
	}
	if m.confirmQuit {
		local = []key.Binding{yesKey, noKey}
	}
	return helpKeys{local: local, global: m.keys.bindings()}
}

func (m *app) View() tea.View {
	tabs := m.tabBar()
	status := m.statusBar()
	helpLine := m.help.View(m.bindings())

	// Tabs, a blank line, the body, the status bar and the help.
	bodyHeight := m.height - 3 - lipgloss.Height(helpLine)

	var body string
	switch {
	case m.confirmQuit:
		body = m.quitDialog()
	default:
		body = m.screens[m.current].view(m.width, bodyHeight)
	}

	if m.showPanel {
		panelHeight := max(bodyHeight/2, 5)
		panel := m.panel.view(m.st, m.spin.View(), m.width, panelHeight)
		body = fit(body, bodyHeight-lipgloss.Height(panel)) + "\n" + panel
	}

	content := strings.Join([]string{tabs, "", fit(body, bodyHeight), status, helpLine}, "\n")
	v := tea.NewView(content)
	// The alternate screen gives the shell's scrollback back untouched on exit.
	v.AltScreen = true
	v.WindowTitle = "fft"
	return v
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
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
