package tui

import (
	"encoding/json"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// The statuses `fft component list` reports.
const (
	statusInstalled = "installed"
	statusAvailable = "available"
)

// emulatorComponent is the component that serves the offline API. The Projects
// screen's emulator pane asks this screen about it by name.
const emulatorComponent = "emulator"

// componentRow is one entry of `fft component list -o json`, which is also the
// document `fft component info` renders. The two being the same is why pressing
// enter here costs no second run: the detail view is this row, drawn in full.
type componentRow struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Version     string   `json:"version"`
	Status      string   `json:"status"`
	Source      string   `json:"source"`
	Description string   `json:"description"`
	Origin      string   `json:"origin"`
	Commands    []string `json:"commands"`
	Targets     []string `json:"targets"`
}

func (r componentRow) installed() bool { return r.Status == statusInstalled }

type componentKeys struct {
	up      key.Binding
	down    key.Binding
	info    key.Binding
	install key.Binding
	upgrade key.Binding
	remove  key.Binding
	reload  key.Binding
	back    key.Binding
	submit  key.Binding
}

func newComponentKeys() componentKeys {
	return componentKeys{
		up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "select")),
		down:    key.NewBinding(key.WithKeys("down", "j")),
		info:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		install: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "install")),
		upgrade: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upgrade")),
		remove:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove")),
		reload:  key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reload")),
		back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		submit:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "install it")),
	}
}

// componentsScreen lists the components fft can see and manages them, each action
// through the fft command that does it.
//
// It asks nothing itself. `component install`, `upgrade` and `remove` each ask
// their own question — they carry the confirms annotation — and the runner hands
// that question to the UI, so no --yes is ever added here: the user confirms the
// command's own words about what it is going to do.
//
// Nothing here is a tenant write, so the read-only gate does not apply and no
// screen of this one shows a write badge. Installing a component changes this
// machine, not the tenant; what the gate does refuse is *running* a component that
// says it makes requests.
type componentsScreen struct {
	s    *session
	st   styles
	keys componentKeys

	rows   []componentRow
	cursor int

	// requested and loaded are whether the list has been asked for and whether an
	// answer has arrived; gen counts the reads, so only the last one's answer lands.
	requested bool
	loaded    bool
	gen       uint64

	// detail is set while one component is shown in full instead of the list.
	detail bool

	notice  string
	failure *failure

	// prompt is the install form, open or nil.
	prompt *installPrompt
}

func newComponentsScreen(s *session, st styles) *componentsScreen {
	return &componentsScreen{s: s, st: st, keys: newComponentKeys()}
}

func (c *componentsScreen) focused() bool { return c.prompt != nil }

// want reads the list unless it has been asked for already. It is called while a
// screen that uses it is on display, so nothing is read until somebody looks.
func (c *componentsScreen) want() tea.Cmd {
	if c.requested {
		return nil
	}
	return c.load(true)
}

// component is what the list says about the component called name, and whether
// the list has been read at all. It is how the emulator pane knows whether the
// emulator is installed without a read of its own.
func (c *componentsScreen) component(name string) (componentRow, bool) {
	if !c.loaded {
		return componentRow{}, false
	}
	for _, r := range c.rows {
		if r.Name == name {
			return r, true
		}
	}
	return componentRow{}, true
}

// load reads `fft component list`. background is a read nobody asked for, which
// the request history leaves out; ctrl+r is the user's own and is recorded.
func (c *componentsScreen) load(background bool) tea.Cmd {
	c.requested = true
	c.gen++
	gen := c.gen

	args := []string{"component", "list"}
	a := action{inv: Invocation{Args: args, Background: background}, display: commandLine(args)}
	return c.s.start(a, func(r Result) tea.Cmd {
		if gen != c.gen {
			return nil
		}
		if r.ExitCode != exitcode.OK {
			c.fail("listing the components", r)
			return nil
		}
		var rows []componentRow
		if err := json.Unmarshal(r.Stdout, &rows); err != nil {
			c.fail("reading the component list", Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())})
			return nil
		}
		c.rows, c.loaded, c.failure = rows, true, nil
		c.selected()
		return nil
	})
}

func (c *componentsScreen) selected() (componentRow, bool) {
	if len(c.rows) == 0 {
		return componentRow{}, false
	}
	c.cursor = min(max(c.cursor, 0), len(c.rows)-1)
	return c.rows[c.cursor], true
}

func (c *componentsScreen) succeed(notice string) {
	c.notice, c.failure = notice, nil
}

func (c *componentsScreen) fail(what string, r Result) {
	c.notice = ""
	c.failure = &failure{what: what, result: r}
}

func (c *componentsScreen) update(msg tea.Msg) tea.Cmd {
	if c.prompt != nil {
		switch ev, cmd := c.prompt.update(msg); ev {
		case formCancelled:
			c.prompt = nil
			return nil
		case formSubmitted:
			return c.install()
		default:
			return cmd
		}
	}

	keyMsg, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return nil
	}
	return c.listKey(keyMsg)
}

func (c *componentsScreen) listKey(msg tea.KeyPressMsg) tea.Cmd {
	row, ok := c.selected()
	switch {
	case key.Matches(msg, c.keys.back):
		c.detail = false
	case key.Matches(msg, c.keys.up):
		c.cursor--
		c.selected()
	case key.Matches(msg, c.keys.down):
		c.cursor++
		c.selected()
	case key.Matches(msg, c.keys.reload):
		return c.load(false)
	case key.Matches(msg, c.keys.install):
		c.prompt = newInstallPrompt(c.st, suggestedSource(row, ok))
	case !ok:
		return nil
	case key.Matches(msg, c.keys.info):
		c.detail = !c.detail
	case key.Matches(msg, c.keys.upgrade):
		return c.lifecycle("upgrade", row)
	case key.Matches(msg, c.keys.remove):
		return c.lifecycle("remove", row)
	}
	return nil
}

// suggestedSource is what the install form opens on: the selected component's
// name when it is one fft knows but has not installed, which is the case the form
// exists for — `fft component install emulator` with the emulator selected.
func suggestedSource(row componentRow, ok bool) string {
	if ok && !row.installed() {
		return row.Name
	}
	return ""
}

// componentArgs is `fft component <verb> <name>`. A component name comes out of a
// manifest rather than user typing, so one starting with a dash goes after "--",
// where it is only a name and never a flag — the guard projectArgs uses.
func componentArgs(verb, name string) []string {
	args := []string{"component", verb}
	if strings.HasPrefix(name, "-") {
		return append(args, "--", name)
	}
	return append(args, name)
}

// lifecycle runs upgrade or remove on row. The command asks its own question
// first, which the UI puts to the user; no --yes is added here.
func (c *componentsScreen) lifecycle(verb string, row componentRow) tea.Cmd {
	if !row.installed() {
		c.succeed(row.Name + " is not installed, so there is nothing to " + verb + ". Press a to install it.")
		return nil
	}
	args := componentArgs(verb, row.Name)
	a := action{inv: Invocation{Args: args}, display: commandLine(args)}
	c.succeed(gerund(verb) + " " + row.Name + "…")

	var id RunID
	id, cmd := c.s.launch(a, func(r Result) tea.Cmd {
		switch {
		case r.ExitCode == exitcode.OK:
			c.succeed(pastTense(verb) + " " + row.Name + ".")
		case id != 0 && c.s.wasDeclined(id):
			// The command asked, and the user said no: nothing happened, and that is
			// not a failure to report as one.
			c.succeed("Nothing was changed.")
			return nil
		default:
			c.fail(strings.ToLower(gerund(verb))+" "+row.Name, r)
		}
		// Either way the root may have changed — an upgrade that failed half way
		// leaves the old version — so the list is read again.
		return c.load(true)
	})
	return cmd
}

func (c *componentsScreen) install() tea.Cmd {
	src := c.prompt.value()
	if src == "" {
		c.prompt.problem = "Name a component to install, or a directory to install from."
		return nil
	}
	args := installArgs(src)
	a := action{inv: Invocation{Args: args}, display: commandLine(args)}

	// Closed as the install is sent, not when it answers. `component install` asks
	// before it unpacks anything, and a question cannot be put to someone who is
	// inside a form: the form has the keyboard, so the question would wait behind it
	// for a form that is waiting on the question. Its work is done once submitted.
	c.prompt = nil
	c.succeed("Installing " + src + "…")

	var id RunID
	id, cmd := c.s.launch(a, func(r Result) tea.Cmd {
		switch {
		case r.ExitCode == exitcode.OK:
			// The runner re-reads the component root after an install, so the commands it
			// adds are in the tree the very next run builds; see cliRunner.rescanComponents.
			c.succeed("Installed " + src + ".")
		case id != 0 && c.s.wasDeclined(id):
			c.succeed("Nothing was installed.")
			return nil
		default:
			c.fail("installing "+src, r)
		}
		return c.load(true)
	})
	return cmd
}

// installArgs is the command line for a typed source.
//
// A local directory is spelled the way a shell spells one — ./x, ../x, /x or ~/x
// — and becomes --path; anything else is a name or an owner/repo[@version] and
// goes as the argument. The two cannot be told apart otherwise, since owner/repo
// has a slash in it exactly like a relative path does.
func installArgs(src string) []string {
	if isLocalPath(src) {
		return []string{"component", "install", "--path", src}
	}
	if strings.HasPrefix(src, "-") {
		return []string{"component", "install", "--", src}
	}
	return []string{"component", "install", src}
}

func isLocalPath(src string) bool {
	switch {
	case strings.HasPrefix(src, "./"), strings.HasPrefix(src, "../"):
		return true
	case strings.HasPrefix(src, "/"), strings.HasPrefix(src, "~"):
		return true
	case src == ".", src == "..":
		return true
	}
	return false
}

// gerund and pastTense spell the two lifecycle verbs, because "removeing" is not
// a word and a notice the user reads should be one.
func gerund(verb string) string {
	switch verb {
	case "upgrade":
		return "Upgrading"
	case "remove":
		return "Removing"
	}
	return verb
}

func pastTense(verb string) string {
	switch verb {
	case "upgrade":
		return "Upgraded"
	case "remove":
		return "Removed"
	}
	return verb
}

func (c *componentsScreen) bindings() []key.Binding {
	if c.prompt != nil {
		return c.prompt.bindings()
	}
	k := c.keys
	if c.detail {
		return []key.Binding{k.back, k.install, k.upgrade, k.remove}
	}
	return []key.Binding{k.up, k.info, k.install, k.upgrade, k.remove, k.reload}
}

func (c *componentsScreen) legend() []legendSection {
	k := c.keys
	main := legendSection{entries: []legendEntry{
		{of: k.up, desc: "select a component"},
		{of: k.info, desc: "show everything the manifest says about it, and back"},
		{of: k.install, desc: "install one (fft component install)"},
		{of: k.upgrade, desc: "upgrade the selected one (fft component upgrade)"},
		{of: k.remove, desc: "remove the selected one (fft component remove)"},
		{of: k.reload, desc: "read the list again"},
		{of: k.back, desc: "close the details"},
	}}
	f := newInstallPromptKeys()
	return []legendSection{main, {
		title:   "In the install form",
		compact: true,
		entries: []legendEntry{
			{of: k.submit, desc: "install what is typed"},
			{of: f.cancel, desc: "cancel"},
		},
	}}
}

func (c *componentsScreen) equivalent() shellCommand {
	if c.prompt != nil {
		return commandLine(installArgs(c.prompt.value()))
	}
	if row, ok := c.selected(); ok {
		if c.detail {
			return commandLine(componentArgs("info", row.Name))
		}
		if !row.installed() {
			return commandLine([]string{"component", "install", row.Name})
		}
	}
	return commandLine([]string{"component", "list"})
}

// view draws the list, or one component in full. The height is the app's to
// enforce: it cuts the body to the rows there are, and this screen has nothing
// that needs to know how many that is.
func (c *componentsScreen) view(width, _ int) string {
	st := c.st
	if c.prompt != nil {
		return c.prompt.view(st, width)
	}

	lines := []string{st.title.Render("Components")}
	lines = append(lines, "")

	row, ok := c.selected()
	switch {
	case !c.loaded && c.failure == nil:
		lines = append(lines, st.dim.Render("Loading components…"))
	case c.loaded && len(c.rows) == 0:
		lines = append(lines, "No components are installed. Press a to install one.")
	case c.detail && ok:
		lines = append(lines, c.detailView(row, width)...)
	default:
		lines = append(lines, c.table(width)...)
	}

	lines = append(lines, "")
	if c.notice != "" {
		lines = append(lines, wrap(st.okText.Render(output.SanitizeCell(c.notice)), width))
	}
	if c.failure != nil {
		lines = append(lines, c.failure.view(st, width))
	}
	return strings.Join(lines, "\n")
}

var componentColumns = []string{"NAME", "KIND", "VERSION", "STATUS", "ORIGIN", "SOURCE"}

func (c *componentsScreen) table(width int) []string {
	cells := make([][]string, 0, len(c.rows)+1)
	cells = append(cells, componentColumns)
	for _, r := range c.rows {
		cells = append(cells, []string{
			output.SanitizeCell(r.Name),
			output.SanitizeCell(r.Kind),
			output.SanitizeCell(r.Version),
			output.SanitizeCell(r.Status),
			output.SanitizeCell(r.Origin),
			output.SanitizeCell(r.Source),
		})
	}
	return c.rowsOf(cells, width)
}

// rowsOf lays cells out in aligned columns, marking the selected row.
func (c *componentsScreen) rowsOf(cells [][]string, width int) []string {
	widths := make([]int, len(cells[0]))
	for _, row := range cells {
		for i, cell := range row {
			widths[i] = max(widths[i], ansi.StringWidth(cell))
		}
	}

	lines := make([]string, 0, len(cells))
	for i, row := range cells {
		var b strings.Builder
		for j, cell := range row {
			b.WriteString(cell)
			if j < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-ansi.StringWidth(cell)+2))
			}
		}
		line := b.String()
		switch {
		case i == 0:
			line = c.st.dim.Render("  " + line)
		case i-1 == c.cursor:
			line = c.st.selected.Render("> " + line)
		default:
			line = "  " + line
		}
		lines = append(lines, clip(line, width))
	}
	return lines
}

// detailView is everything the manifest says about row. It is the document
// `fft component info` prints, drawn from the row already read.
func (c *componentsScreen) detailView(row componentRow, width int) []string {
	st := c.st
	lines := []string{st.title.Render(output.SanitizeCell(row.Name))}
	if row.Description != "" {
		lines = append(lines, wrap(output.SanitizeCell(row.Description), width), "")
	}

	field := func(label, value string) {
		if value != "" {
			lines = append(lines, st.dim.Render(label+": ")+output.SanitizeCell(value))
		}
	}
	field("Kind", row.Kind)
	field("Status", row.Status)
	field("Version", row.Version)
	field("Origin", row.Origin)
	field("Source", row.Source)
	if len(row.Commands) > 0 {
		field("Commands", strings.Join(row.Commands, ", "))
	}
	if len(row.Targets) > 0 {
		field("Delivers", strings.Join(row.Targets, ", "))
	}

	if !row.installed() {
		lines = append(lines, "", wrap(st.warnText.Render(
			"Not installed. Press a to install it; until then its commands are in the tree only to explain themselves."), width))
	}
	return lines
}
