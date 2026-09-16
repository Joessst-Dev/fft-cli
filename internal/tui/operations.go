package tui

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/paginator"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// Column widths of the operations list. A tag or an id longer than its column is
// cut, and the describe view shows it whole.
const (
	opTagWidth = 26
	opIDWidth  = 36
)

// opHint is what the list knows about an operation beyond the API's description of
// it: how often this project has sent it, and which permissions the user appears
// to lack for it. Both come from the request history and the user's roles, which a
// later release reads; until then every hint is the zero value, and the list
// draws neither.
type opHint struct {
	uses    int
	lacking []string
}

// noHints is the hint source while there is none.
func noHints(Operation) opHint { return opHint{} }

// navigator moves the UI between screens on a screen's behalf.
type navigator interface {
	// openRequest shows the Request screen with a form for op.
	openRequest(op Operation) tea.Cmd

	// openOperations shows the Operations screen.
	openOperations() tea.Cmd

	// openRequestScreen shows the Request screen as it was left.
	openRequestScreen() tea.Cmd

	// openResponse shows the Response screen with run id.
	openResponse(id RunID)

	// showing reports whether scr is the screen on display.
	showing(scr screen) bool
}

// opItem is one operation in the list.
type opItem struct {
	op     Operation
	filter string
}

func (i opItem) FilterValue() string { return i.filter }

func newOpItem(op Operation) opItem {
	// What a user remembers an operation by: its id, what it does, or the command
	// they would type.
	filter := op.ID + " " + op.Summary + " fft " + strings.Join(op.Command.Path, " ")
	return opItem{op: op, filter: filter}
}

type operationKeys struct {
	up       key.Binding
	filter   key.Binding
	open     key.Binding
	describe key.Binding
	back     key.Binding
	clear    key.Binding
	accept   key.Binding
	cancel   key.Binding
}

func newOperationKeys() operationKeys {
	return operationKeys{
		up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "select")),
		filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "request")),
		describe: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "describe")),
		back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		clear:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear search")),
		accept:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
		cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// operationsScreen lists every operation, grouped by tag, and searches them.
type operationsScreen struct {
	s    *session
	st   styles
	nav  navigator
	keys operationKeys
	list list.Model

	// describing is the operation whose description is open, nil when the list is
	// showing.
	describing *Operation

	// hint is where the list learns what it shows beyond the spec. See [opHint].
	hint func(Operation) opHint
}

func newOperationsScreen(s *session, st styles, nav navigator, cat Catalog) *operationsScreen {
	o := &operationsScreen{s: s, st: st, nav: nav, keys: newOperationKeys(), hint: noHints}

	var items []list.Item
	if cat != nil {
		for _, g := range cat.Groups() {
			for _, op := range g.Operations {
				items = append(items, newOpItem(op))
			}
		}
	}

	l := list.New(items, opDelegate{o: o}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetStatusBarItemName("operation", "operations")
	l.Styles = listStyles(st)
	l.FilterInput.Prompt = "Search: "
	l.FilterInput.SetStyles(l.Styles.Filter)
	if !st.color {
		l.Paginator.Type = paginator.Arabic
	}
	// The UI's own keys decide quitting and help; the list's would act first.
	l.DisableQuitKeybindings()
	l.KeyMap.ForceQuit = key.NewBinding()
	l.KeyMap.ShowFullHelp = key.NewBinding()
	l.KeyMap.CloseFullHelp = key.NewBinding()
	o.list = l
	return o
}

// listStyles are the list's styles in the UI's colours, or none.
func listStyles(st styles) list.Styles {
	if st.color {
		ls := list.DefaultStyles(true)
		ls.Filter = steadyCursor(ls.Filter)
		return ls
	}
	ls := list.Styles{
		TitleBar:  st.dim.Padding(0, 0, 1, 2),
		StatusBar: st.dim.Padding(0, 0, 1, 2),
		Filter:    plainInputStyles(),
		// Underlining is not colour: it still shows what matched.
		DefaultFilterCharacterMatch: st.dim.Underline(true),
		PaginationStyle:             st.dim.PaddingLeft(2),
		DividerDot:                  st.dim.SetString(" • "),
	}
	return ls
}

func (o *operationsScreen) focused() bool {
	return o.list.FilterState() == list.Filtering
}

func (o *operationsScreen) selected() (Operation, bool) {
	item, ok := o.list.SelectedItem().(opItem)
	return item.op, ok
}

func (o *operationsScreen) update(msg tea.Msg) tea.Cmd {
	keyMsg, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		if o.focused() {
			// Pasted text is typed into the search.
			var cmd tea.Cmd
			o.list, cmd = o.list.Update(msg)
			return cmd
		}
		return nil
	}

	if o.describing != nil {
		switch {
		case key.Matches(keyMsg, o.keys.back):
			o.describing = nil
		case key.Matches(keyMsg, o.keys.open):
			op := *o.describing
			o.describing = nil
			return o.nav.openRequest(op)
		}
		return nil
	}

	if !o.focused() {
		switch {
		case key.Matches(keyMsg, o.keys.open):
			if op, ok := o.selected(); ok {
				return o.nav.openRequest(op)
			}
			return nil
		case key.Matches(keyMsg, o.keys.describe):
			if op, ok := o.selected(); ok {
				o.describing = &op
			}
			return nil
		}
	}

	var cmd tea.Cmd
	o.list, cmd = o.list.Update(keyMsg)
	return cmd
}

// receive takes the list's own messages: the result of a search, which it runs in
// the background.
func (o *operationsScreen) receive(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(list.FilterMatchesMsg); !ok {
		return nil
	}
	var cmd tea.Cmd
	o.list, cmd = o.list.Update(msg)
	return cmd
}

func (o *operationsScreen) bindings() []key.Binding {
	k := o.keys
	switch {
	case o.describing != nil:
		return []key.Binding{k.open, k.back}
	case o.focused():
		return []key.Binding{k.accept, k.cancel}
	case o.list.FilterState() == list.FilterApplied:
		return []key.Binding{k.up, k.open, k.describe, k.filter, k.clear}
	default:
		return []key.Binding{k.up, k.open, k.describe, k.filter}
	}
}

func (o *operationsScreen) equivalent() shellCommand {
	op, ok := o.selected()
	if o.describing != nil {
		op, ok = *o.describing, true
	}
	if !ok {
		return shellCommand{}
	}
	return o.s.displayFor(op.Command.Path, o.s.project)
}

// resize tells the list the size of the body it is drawn in, below its title. The
// list pages by its own height, so it must know it before a key moves it.
func (o *operationsScreen) resize(width, height int) {
	if width > 0 && height > 0 && (o.list.Width() != width || o.list.Height() != max(height-2, 1)) {
		o.list.SetSize(width, max(height-2, 1))
	}
}

func (o *operationsScreen) view(width, _ int) string {
	if o.describing != nil {
		return o.describe(*o.describing, width)
	}

	title := o.st.title.Render("Operations")
	if o.s.readOnly() {
		title += "  " + o.st.dim.Render("read-only: a write marked "+lockBadge+" would be refused")
	}
	return title + "\n\n" + o.list.View()
}

// lockBadge marks a write that the current project or session refuses. It is two
// cells wide, like every emoji a terminal draws.
const lockBadge = "🔒"

// badges are an operation's markers, three cells wide: W for a write, and a lock
// for a write the current project or session refuses. Neither needs colour to be
// read.
func (o *operationsScreen) badges(op Operation) string {
	switch {
	case !op.Mutates:
		return "   "
	case o.s.readOnly():
		return "W" + lockBadge
	default:
		return "W  "
	}
}

// describe is the whole of what the UI knows about op.
func (o *operationsScreen) describe(op Operation, width int) string {
	st := o.st
	clean := output.SanitizeCell
	lines := []string{
		st.title.Render(clean(firstNonEmpty(op.Summary, op.ID))),
		"",
		clean(op.Method + " " + op.Path),
		"operation  " + clean(op.ID),
		"tag        " + clean(op.Tag),
		"command    " + o.s.displayFor(op.Command.Path, "").String() + commandKind(op.Command),
	}

	switch {
	case !op.Mutates:
		lines = append(lines, "access     read")
	case o.s.readOnly():
		lines = append(lines, "access     "+st.warnText.Render("write — refused here: this project or session is read-only"))
	default:
		lines = append(lines, "access     write — sending it asks first")
	}
	if op.Deprecated {
		lines = append(lines, st.warnText.Render("deprecated: the API may remove it"))
	}

	perms := "none documented"
	if len(op.Permissions) > 0 {
		perms = "any of " + clean(strings.Join(op.Permissions, ", "))
	}
	lines = append(lines, "permission "+perms)

	switch cmd := op.Command; {
	case !cmd.Body:
		lines = append(lines, "body       none")
	case cmd.BodyRequired:
		lines = append(lines, "body       required, as JSON on stdin")
	default:
		lines = append(lines, "body       optional, as JSON on stdin")
	}

	if args := op.Command.Args; len(args) > 0 {
		lines = append(lines, "", st.title.Render("Arguments"))
		for _, a := range args {
			lines = append(lines, "  <"+clean(a.Name)+">"+requiredMark(a.Required))
		}
	}
	if flags := op.Command.Flags; len(flags) > 0 {
		lines = append(lines, "", st.title.Render("Flags"))
		for _, f := range flags {
			lines = append(lines, clip("  "+describeFlagLine(f), width))
		}
	}
	if op.Description != "" {
		lines = append(lines, "", wrap(output.Sanitize(op.Description), width))
	}
	return strings.Join(lines, "\n")
}

func commandKind(cmd Command) string {
	if cmd.Curated {
		return " (hand-written)"
	}
	return " (generated from the API's schema)"
}

func requiredMark(required bool) string {
	if required {
		return " (required)"
	}
	return ""
}

// describeFlagLine is a flag as the describe view and the form's hint show it.
func describeFlagLine(f Flag) string {
	var b strings.Builder
	b.WriteString("--" + f.Name + " " + kindName(f.Kind) + requiredMark(f.Required))
	if f.Usage != "" {
		b.WriteString("  " + f.Usage)
	}
	if len(f.Enum) > 0 {
		b.WriteString("  one of: " + strings.Join(f.Enum, ", "))
	}
	if f.Default != "" {
		b.WriteString("  default: " + f.Default)
	}
	return output.SanitizeCell(b.String())
}

func kindName(k FlagKind) string {
	switch k {
	case FlagBool:
		return "on/off"
	case FlagInt:
		return "integer"
	case FlagFloat:
		return "number"
	case FlagDuration:
		return "duration"
	case FlagList:
		return "list"
	case FlagPairs:
		return "name=value pairs"
	default:
		return "text"
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// opDelegate draws one operation per line.
type opDelegate struct {
	o *operationsScreen
}

func (d opDelegate) Height() int                         { return 1 }
func (d opDelegate) Spacing() int                        { return 0 }
func (d opDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d opDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(opItem)
	if !ok {
		return
	}
	o, op := d.o, it.op
	st := o.st
	selected := index == m.Index()

	// A tag is written on the first row of its group, and on the first row of a
	// page, so that every page says which group it is in.
	tag := op.Tag
	first := index == m.Paginator.Page*m.Paginator.PerPage
	if !first && index > 0 {
		if prev, ok := m.VisibleItems()[index-1].(opItem); ok && prev.op.Tag == tag {
			tag = ""
		}
	}

	hint := o.hint(op)
	star := ""
	if hint.uses > 0 {
		star = " ★" + strconv.Itoa(hint.uses)
	}

	marker := "  "
	if selected {
		marker = "> "
	}
	line := marker +
		pad(output.SanitizeCell(tag), opTagWidth) + " " +
		o.badges(op) + " " +
		pad(output.SanitizeCell(op.ID), opIDWidth) +
		star + "  " +
		output.SanitizeCell(op.Summary)
	command := "  fft " + output.SanitizeCell(strings.Join(op.Command.Path, " "))

	line = clip(line+st.dim.Render(command), m.Width())
	switch {
	case selected:
		line = st.selected.Render(line)
	case len(hint.lacking) > 0:
		// A hint only: roles are scoped to facilities the list cannot see, so a
		// dimmed operation can still be sent.
		line = st.dim.Render(line)
	}
	_, _ = fmt.Fprint(w, line)
}

// pad cuts s to width cells, or pads it to width with spaces.
func pad(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}
