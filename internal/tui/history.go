package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/history"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// History is the request history fft records, read where fft keeps it.
//
// It is read directly rather than through `fft history list`: the UI needs every
// entry, as recorded, to count the operations it lists, and why nothing new is
// being recorded, which the command only says in prose on stderr.
type History interface {
	// Read returns every recorded request, oldest first. off says why requests are
	// not being recorded now, "" when they are; what was recorded before is
	// returned either way. It is called from a goroutine of its own.
	Read() (entries []history.Entry, off string, err error)
}

// historyReadMsg is what a read of the history found.
type historyReadMsg struct {
	entries []history.Entry
	off     string
	err     error
}

type historyKeys struct {
	up     key.Binding
	down   key.Binding
	open   key.Binding
	toggle key.Binding
	clear  key.Binding
	reload key.Binding
}

func newHistoryKeys() historyKeys {
	return historyKeys{
		up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "select")),
		down:   key.NewBinding(key.WithKeys("down", "j")),
		open:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "request")),
		toggle: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "recent/most used")),
		clear:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear")),
		reload: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reload")),
	}
}

// historyScreen lists the requests sent to the current project, newest first, or
// the operations used most. It also keeps the counts the Operations list stars.
type historyScreen struct {
	s    *session
	st   styles
	nav  navigator
	keys historyKeys
	src  History

	// ops is every operation the catalog offers, by id: an entry names the
	// operation, and the catalog knows the form that sends it.
	ops map[string]Operation

	// entries is every recorded request, and off why none are being recorded. read
	// is set once a read has answered, and gen counts those that did.
	entries []history.Entry
	off     string
	err     error
	read    bool
	gen     uint64

	// stale says the file may have changed since it was read: a run was recorded,
	// or the history was cleared. reading is set while a read is on its way.
	stale   bool
	reading bool

	// recent, top and uses are the entries for derived's project, worked out once
	// for each read and each project rather than on every frame.
	derived derivedFor
	recent  []history.Entry
	top     []history.Usage
	uses    map[string]int

	mostUsed bool
	cursor   int

	dialog  dialog
	notice  string
	failure *failure
}

// derivedFor is what the derived lists were worked out from.
type derivedFor struct {
	gen     uint64
	project string
}

func newHistoryScreen(s *session, st styles, nav navigator, cat Catalog, src History) *historyScreen {
	h := &historyScreen{
		s: s, st: st, nav: nav, keys: newHistoryKeys(), src: src,
		ops:   make(map[string]Operation),
		stale: true,
	}
	if cat != nil {
		for _, g := range cat.Groups() {
			for _, op := range g.Operations {
				h.ops[op.ID] = op
			}
		}
	}
	return h
}

func (h *historyScreen) focused() bool { return h.dialog != nil }

// runFinished says a run has ended and was recorded.
func (h *historyScreen) runFinished() { h.stale = true }

// want reads the history if it may have changed since it was last read. It is
// called while a screen that shows it is on display; a read already on its way
// is not doubled, and the next call after it answers reads again if a run was
// recorded meanwhile.
func (h *historyScreen) want() tea.Cmd {
	if h.src == nil || !h.stale || h.reading {
		return nil
	}
	h.stale, h.reading = false, true
	src := h.src
	return func() (msg tea.Msg) {
		// A read that panics must still answer: the screen waits for an answer before
		// it reads again, and the panic would otherwise end the whole UI.
		defer func() {
			if r := recover(); r != nil {
				msg = historyReadMsg{err: fmt.Errorf("reading the history panicked: %v", r)}
			}
		}()
		entries, off, err := src.Read()
		return historyReadMsg{entries: entries, off: off, err: err}
	}
}

func (h *historyScreen) receive(msg tea.Msg) tea.Cmd {
	read, ok := msg.(historyReadMsg)
	if !ok {
		return nil
	}
	h.reading, h.read = false, true
	h.gen++
	h.off, h.err = read.off, read.err
	if read.err == nil {
		h.entries = read.entries
	}
	return nil
}

// sync works the lists out again when the history or the current project has
// changed since they were. It is cheap when neither has.
func (h *historyScreen) sync() {
	want := derivedFor{gen: h.gen, project: h.s.currentProject()}
	if h.derived == want {
		return
	}
	previousOp := h.selectedOperation()
	previousEntry, hadEntry := entryIdentity{}, false
	if !h.mostUsed && h.cursor >= 0 && h.cursor < len(h.recent) {
		previousEntry, hadEntry = identify(h.recent[h.cursor]), true
	}
	h.derived = want

	h.recent = h.recent[:0]
	for _, e := range slices.Backward(h.entries) {
		if want.project == "" || e.Project == want.project {
			h.recent = append(h.recent, e)
		}
	}
	h.uses = make(map[string]int)
	h.top = nil
	if want.project != "" {
		// Counted per project: without one, two projects' uses of an operation would
		// be two rows of the same id.
		h.top = history.Top(h.entries, want.project, 0)
		for _, u := range h.top {
			h.uses[u.OperationID] = u.Count
		}
	}

	// The cursor stays on the row it was on, where there still is one: the newest
	// request moves every row down. A row of recent requests is that request, not
	// its operation — most operations are sent many times, and enter must reopen
	// the one the user picked.
	h.cursor = 0
	switch {
	case hadEntry:
		for i, e := range h.recent {
			if identify(e) == previousEntry {
				h.cursor = i
				break
			}
		}
	case previousOp != "":
		for i := range h.rowCount() {
			if h.operationAt(i) == previousOp {
				h.cursor = i
				break
			}
		}
	}
}

// entryIdentity tells one recorded request from another. An entry has no id of
// its own, and two requests that agree on all of this are the same request as far
// as reopening one goes.
type entryIdentity struct {
	ts          int64
	project     string
	operationID string
	command     string
	args        string
	exit        int
}

func identify(e history.Entry) entryIdentity {
	return entryIdentity{
		ts:          e.TS.UnixNano(),
		project:     e.Project,
		operationID: e.OperationID,
		command:     e.Command,
		// A NUL cannot be part of an argument, so no two argument lists join alike.
		args: strings.Join(e.Args, "\x00"),
		exit: e.Exit,
	}
}

// usesOf is how often op was sent to the current project, as last read.
func (h *historyScreen) usesOf(id string) int {
	if h.derived.project != h.s.currentProject() {
		return 0
	}
	return h.uses[id]
}

func (h *historyScreen) rowCount() int {
	if h.mostUsed {
		return len(h.top)
	}
	return len(h.recent)
}

func (h *historyScreen) operationAt(i int) string {
	switch {
	case i < 0 || i >= h.rowCount():
		return ""
	case h.mostUsed:
		return h.top[i].OperationID
	default:
		return h.recent[i].OperationID
	}
}

func (h *historyScreen) selectedOperation() string {
	return h.operationAt(h.cursor)
}

// selectedEntry is the request the selected row stands for: the row itself, or
// for an operation used most, the last time it was sent.
func (h *historyScreen) selectedEntry() (history.Entry, bool) {
	if h.cursor < 0 || h.cursor >= h.rowCount() {
		return history.Entry{}, false
	}
	if !h.mostUsed {
		return h.recent[h.cursor], true
	}
	id := h.top[h.cursor].OperationID
	for _, e := range h.recent {
		if e.OperationID == id {
			return e, true
		}
	}
	return history.Entry{}, false
}

func (h *historyScreen) say(notice string) {
	h.notice, h.failure = notice, nil
}

func (h *historyScreen) update(msg tea.Msg) tea.Cmd {
	if d := h.dialog; d != nil {
		finished, cmd := d.update(msg)
		if finished && h.dialog == d {
			h.dialog = nil
		}
		return cmd
	}
	keyMsg, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return nil
	}
	switch {
	case key.Matches(keyMsg, h.keys.up):
		h.cursor = max(h.cursor-1, 0)
	case key.Matches(keyMsg, h.keys.down):
		h.cursor = max(min(h.cursor+1, h.rowCount()-1), 0)
	case key.Matches(keyMsg, h.keys.toggle):
		h.mostUsed = !h.mostUsed
		h.cursor = 0
		h.say("")
	case key.Matches(keyMsg, h.keys.reload):
		h.stale = true
		return h.want()
	case key.Matches(keyMsg, h.keys.clear):
		return h.clear()
	case key.Matches(keyMsg, h.keys.open):
		if e, ok := h.selectedEntry(); ok {
			return h.reopen(e)
		}
	}
	return nil
}

// reopen fills in the Request form the way e was sent. A form that holds work the
// user has not sent is only replaced once they say so.
func (h *historyScreen) reopen(e history.Entry) tea.Cmd {
	op, ok := h.ops[e.OperationID]
	if !ok {
		h.say("This fft has no operation " + output.SanitizeCell(e.OperationID) +
			": the API may have changed since the request was sent.")
		return nil
	}
	op, rc := recall(op, e)
	form, lost := h.nav.unsentForm(nil)
	if lost == "" {
		return h.nav.openRecalled(op, rc)
	}
	h.dialog = armed(&confirmDialog{
		question: fmt.Sprintf("Replace the Request form for %s?", form),
		notes:    []string{"It holds " + lost + " that you have not sent. Replacing the form loses them."},
		detail:   "Nothing is sent: the form is filled in, and waits for you.",
		onYes:    func() tea.Cmd { return h.nav.openRecalled(op, rc) },
	}, h.s.now, true)
	return nil
}

// clear runs `history clear`, which asks its own question before it deletes
// anything.
func (h *historyScreen) clear() tea.Cmd {
	args := []string{"history", "clear"}
	h.say("Clearing the history…")
	var id RunID
	id, cmd := h.s.launch(action{inv: Invocation{Args: args}, display: commandLine(args)}, func(r Result) tea.Cmd {
		switch {
		case r.ExitCode == exitcode.OK:
			h.say(firstNonEmpty(strings.Join(stderrTail(r.Stderr, 1), ""), "Cleared the history."))
		case id != 0 && h.s.wasDeclined(id):
			h.say("Nothing was cleared.")
		default:
			h.notice = ""
			h.failure = &failure{what: "clearing the history", result: r}
		}
		h.stale = true
		return nil
	})
	return cmd
}

func (h *historyScreen) bindings() []key.Binding {
	if h.dialog != nil {
		return h.dialog.bindings()
	}
	k := h.keys
	if h.rowCount() == 0 {
		return []key.Binding{k.toggle, k.clear, k.reload}
	}
	return []key.Binding{k.up, k.open, k.toggle, k.clear, k.reload}
}

func (h *historyScreen) equivalent() shellCommand {
	if h.dialog != nil {
		return h.dialog.equivalent()
	}
	verb := "list"
	if h.mostUsed {
		verb = "top"
	}
	return h.s.displayFor([]string{"history", verb}, h.s.currentProject())
}

func (h *historyScreen) view(width, height int) string {
	st := h.st
	title := "History · recent requests"
	if h.mostUsed {
		title = "History · most used operations"
	}
	lines := []string{st.title.Render(title)}
	project := h.s.currentProject()
	switch {
	case project != "":
		lines[0] += st.dim.Render("  on " + output.SanitizeCell(project))
	case !h.mostUsed:
		lines[0] += st.dim.Render("  on every project")
	}
	if h.dialog != nil {
		return strings.Join(append(lines, "", h.dialog.view(st, width, height-2)), "\n")
	}
	if h.off != "" {
		lines = append(lines, wrap(st.warnText.Render(output.SanitizeCell(
			"Requests are not being recorded: "+h.off+".")), width))
	}
	lines = append(lines, "")

	switch {
	case h.src == nil:
		lines = append(lines, "The request history is not available here.")
	case h.err != nil:
		lines = append(lines, (&failure{what: "reading the history", result: Result{
			ExitCode: exitcode.General, Stderr: []byte(h.err.Error()),
		}}).view(st, width))
	case !h.read:
		lines = append(lines, st.dim.Render("Reading the history…"))
	case h.rowCount() == 0 && h.off != "":
		lines = append(lines, wrap("Nothing to show, and nothing is added while history is off: "+
			"this is not a sign that nothing was sent.", width))
	case h.mostUsed && project == "":
		// Counted per project, so there is nothing to count without one; the recent
		// list still has every project's requests.
		lines = append(lines, wrap("The operations used most are counted per project, and no project is selected. "+
			"Choose one on the Projects screen (1), or press t for every project's recent requests.", width))
	case h.rowCount() == 0 && project == "":
		lines = append(lines, "No requests have been recorded yet.")
	case h.rowCount() == 0:
		lines = append(lines, "No requests have been recorded for this project yet.")
	default:
		room := max(height-len(lines)-len(h.footer(width))-1, 2)
		lines = append(lines, h.table(width, room)...)
	}
	return strings.Join(append(lines, h.footer(width)...), "\n")
}

func (h *historyScreen) footer(width int) []string {
	var lines []string
	if h.notice != "" {
		lines = append(lines, "", wrap(h.st.okText.Render(output.SanitizeCell(h.notice)), width))
	}
	if h.failure != nil {
		lines = append(lines, "", h.failure.view(h.st, width))
	}
	return lines
}

// table is the rows, a header and as many as fit in room lines, scrolled to keep
// the cursor in view.
func (h *historyScreen) table(width, room int) []string {
	var cells [][]string
	if h.mostUsed {
		cells = append(cells, []string{"USES", "LAST USED", "OPERATION", "COMMAND"})
		for _, u := range h.top {
			cells = append(cells, []string{
				strconv.Itoa(u.Count),
				u.LastUsed.Local().Format(time.DateTime),
				output.SanitizeCell(u.OperationID),
				output.SanitizeCell(u.Command),
			})
		}
	} else {
		cells = append(cells, []string{"WHEN", "OPERATION", "STATUS", "EXIT", "TOOK", "COMMAND"})
		for _, e := range h.recent {
			status := "-"
			if e.Status != 0 {
				status = strconv.Itoa(e.Status)
			}
			cells = append(cells, []string{
				e.TS.Local().Format(time.DateTime),
				output.SanitizeCell(e.OperationID),
				status,
				strconv.Itoa(e.Exit),
				(time.Duration(e.DurationMS) * time.Millisecond).String(),
				output.SanitizeCell(strings.Join(append([]string{e.Command}, e.Args...), " ")),
			})
		}
	}

	rows := len(cells) - 1
	shown := max(room-1, 1)
	first := min(max(h.cursor-shown+1, 0), max(rows-shown, 0))
	last := min(first+shown, rows)

	// Widths are measured on the rows on screen only: a history can be thousands of
	// requests long.
	visible := append([][]string{cells[0]}, cells[first+1:last+1]...)
	widths := make([]int, len(cells[0]))
	for _, row := range visible {
		for i, c := range row[:len(row)-1] {
			widths[i] = max(widths[i], ansi.StringWidth(c))
		}
	}

	lines := make([]string, 0, len(visible))
	for i, row := range visible {
		var b strings.Builder
		for j, c := range row {
			b.WriteString(c)
			if j < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-ansi.StringWidth(c)+2))
			}
		}
		line := b.String()
		switch {
		case i == 0:
			line = h.st.dim.Render("  " + line)
		case first+i-1 == h.cursor:
			line = h.st.selected.Render("> " + line)
		default:
			line = "  " + line
		}
		lines = append(lines, clip(line, width))
	}
	if rows > shown {
		lines = append(lines, h.st.dim.Render(fmt.Sprintf("  %d–%d of %d", first+1, last, rows)))
	}
	return lines
}

// recalled is what a recorded request gave the fields of its operation's form.
type recalled struct {
	// args are the positional arguments, in order; "" for one history did not keep.
	args []string

	// values are the flags' values as their fields take them, and switches the
	// on/off flags'.
	values   map[string]string
	switches map[string]switchState

	// kept is false when the request was sent through a command the operation no
	// longer has, whose flags no form has: nothing is filled in.
	kept bool
	via  string

	// withheld are the fields, and the values of list fields, that history keeps
	// no value for, which are not filled in.
	withheld []string

	// dropped are the flags the form has no field for.
	dropped []string

	// body says the request had a body, which history never keeps.
	body bool

	sent time.Time
}

// recall reads what e gave each field of the form of the command e was sent
// through, and returns op with that form. When op has no command at e's path, op
// is returned as it is, and nothing is filled in.
func recall(op Operation, e history.Entry) (Operation, recalled) {
	rc := recalled{
		values:   make(map[string]string),
		switches: make(map[string]switchState),
		sent:     e.TS,
		via:      e.Command,
	}

	path := strings.Fields(e.Command)
	if len(path) > 0 && path[0] == "fft" {
		path = path[1:]
	}
	positional, flagArgs := history.SplitArgs(e.Args)
	// `fft api <operationId>` names its operation as its first argument, which is
	// part of the command the form stands for.
	if slices.Equal(path, []string{"api"}) && len(positional) > 0 {
		path = append(path, positional[0])
		positional = positional[1:]
	}
	op, rc.kept = op.sentThrough(path)
	if !rc.kept {
		return op, rc
	}

	flags := make(map[string]Flag, len(op.Command.Flags))
	for _, f := range op.Command.Flags {
		flags[f.Name] = f
	}

	for _, value := range positional {
		if value == history.Redacted {
			value = ""
			if len(rc.args) < len(op.Command.Args) {
				rc.withheld = appendOnce(rc.withheld, "<"+op.Command.Args[len(rc.args)].Name+">")
			}
		}
		rc.args = append(rc.args, value)
	}

	lists := make(map[string][]string)
	for _, arg := range flagArgs {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		f, known := flags[name]
		switch {
		case name == "file" || name == "data":
			rc.body = true
		case !known:
			rc.dropped = appendOnce(rc.dropped, "--"+name)
		case strings.Contains(value, history.Redacted) && (f.Kind == FlagList || f.Kind == FlagPairs):
			// One value of several: the others are kept, and this one is named, so
			// that the user knows what to add rather than guess what went missing.
			rc.withheld = appendOnce(rc.withheld, withheldValue(name, value))
		case strings.Contains(value, history.Redacted):
			// A redaction marker is not a value, and never goes into a field as one.
			rc.withheld = appendOnce(rc.withheld, "--"+name)
		case f.Kind == FlagBool:
			rc.switches[name] = switchOn
			if hasValue {
				if on, err := strconv.ParseBool(value); err == nil && !on {
					rc.switches[name] = switchOff
				}
			}
		case f.Kind == FlagList, f.Kind == FlagPairs:
			lists[name] = append(lists[name], value)
		default:
			rc.values[name] = value
		}
	}
	for name, values := range lists {
		rc.values[name] = strings.Join(values, ",")
	}
	return op, rc
}

// withheldValue names a value of list flag that history did not keep: by the name
// of its pair, where the record still has it, or else as one of the flag's values.
func withheldValue(flag, value string) string {
	if prefix, ok := strings.CutSuffix(value, history.Redacted); ok {
		if pair := strings.TrimRight(prefix, "=: "); pair != "" {
			return "the " + pair + " pair of --" + flag
		}
	}
	return "a value of --" + flag
}

func appendOnce(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// inProse joins items the way a sentence lists them: "a, b and c".
func inProse(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// notice is what the form says about where its values came from.
func (rc recalled) notice() string {
	when := rc.sent.Local().Format(time.DateTime)
	if !rc.kept {
		return fmt.Sprintf("The request of %s was sent as '%s', whose flags this form does not have, "+
			"so the form starts empty.", when, output.SanitizeCell(rc.via))
	}
	return "Filled in from the request of " + when + ". Nothing has been sent."
}

// warnings are what the form could not fill in, each on its own line.
func (rc recalled) warnings() []string {
	var out []string
	if len(rc.withheld) > 0 {
		// A pair's name comes out of the history file, not out of the catalog.
		verb := "it is"
		if len(rc.withheld) > 1 {
			verb = "they are"
		}
		out = append(out, output.SanitizeCell("History keeps no value for "+inProse(rc.withheld)+
			", so "+verb+" not filled in."))
	}
	if rc.body {
		out = append(out, "History keeps no request bodies: press e to write it again.")
	}
	if len(rc.dropped) > 0 {
		// The names come out of the history file, not out of the catalog.
		out = append(out, output.SanitizeCell("Not part of this form, so not carried over: "+
			strings.Join(rc.dropped, ", ")+"."))
	}
	return out
}
