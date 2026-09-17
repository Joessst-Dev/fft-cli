package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// keptRuns is how many finished runs the panel remembers. Enough to scroll back
// over a session's recent work, and a bound on what a long session holds.
const keptRuns = 100

// keptOutputBytes is how much output the session holds across its finished runs.
// The runner already bounds each run; this bounds the hundred of them, newest
// first, so that a session spent paging through large lists does not grow
// without limit. An older run's output beyond it is dropped, and its response
// says so.
const keptOutputBytes = 64 << 20

// runEntry is one invocation as the panel shows it.
type runEntry struct {
	id      RunID
	display shellCommand
	state   RunState
	queued  time.Time
	started time.Time
	ended   time.Time
	result  Result

	// asking is set while the run waits for the user to answer its question.
	asking bool

	// dropped is set once the run's output has been let go to stay within
	// keptOutputBytes.
	dropped bool
}

// runList is every run of the session, oldest first.
type runList struct {
	entries []*runEntry
	byID    map[RunID]*runEntry
}

func newRunList() *runList {
	return &runList{byID: make(map[RunID]*runEntry)}
}

func (l *runList) add(id RunID, display shellCommand, at time.Time) {
	e := &runEntry{id: id, display: display, state: RunQueued, queued: at}
	l.entries = append(l.entries, e)
	l.byID[id] = e
	l.trim()
}

func (l *runList) update(ev RunEvent) {
	e, ok := l.byID[ev.ID]
	if !ok {
		return
	}
	e.state = ev.State
	switch ev.State {
	case RunQueued:
		e.queued = ev.At
	case RunRunning:
		if ev.Question != nil {
			// Asked while it runs: it started when it said so first.
			e.asking = true
			return
		}
		e.started = ev.At
	case RunDone:
		e.asking = false
		e.ended = ev.At
		e.result = ev.Result
		l.shed(keptOutputBytes)
	}
}

// shed drops the output of the oldest finished runs until what is left fits in
// budget.
func (l *runList) shed(budget int) {
	kept := 0
	for i := len(l.entries) - 1; i >= 0; i-- {
		e := l.entries[i]
		size := len(e.result.Stdout) + len(e.result.Stderr)
		if size == 0 {
			continue
		}
		if kept+size <= budget {
			kept += size
			continue
		}
		e.result.Stdout, e.result.Stderr = nil, nil
		e.dropped = true
	}
}

// trim forgets the oldest finished runs beyond keptRuns. A run still in flight is
// never forgotten: it is the one a user may want to cancel.
func (l *runList) trim() {
	excess := len(l.entries) - keptRuns
	if excess <= 0 {
		return
	}
	kept := l.entries[:0]
	for _, e := range l.entries {
		if excess > 0 && e.state == RunDone {
			delete(l.byID, e.id)
			excess--
			continue
		}
		kept = append(kept, e)
	}
	l.entries = kept
}

func (l *runList) inFlight() int {
	n := 0
	for _, e := range l.entries {
		if e.state != RunDone {
			n++
		}
	}
	return n
}

// runsPanel is the drawer listing the session's runs, newest first.
type runsPanel struct {
	s      *session
	cursor int
	keys   runsKeys
}

type runsKeys struct {
	up     key.Binding
	down   key.Binding
	open   key.Binding
	cancel key.Binding
	close  key.Binding
}

func newRunsPanel(s *session) *runsPanel {
	return &runsPanel{
		s: s,
		keys: runsKeys{
			up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "select")),
			down:   key.NewBinding(key.WithKeys("down", "j")),
			open:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open response")),
			cancel: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cancel run")),
			close:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
		},
	}
}

// newestFirst is the order the panel lists runs in: what just happened is what
// the user opened the panel to see.
func (p *runsPanel) newestFirst() []*runEntry {
	all := p.s.runs.entries
	out := make([]*runEntry, len(all))
	for i, e := range all {
		out[len(all)-1-i] = e
	}
	return out
}

func (p *runsPanel) selected() *runEntry {
	runs := p.newestFirst()
	if len(runs) == 0 {
		return nil
	}
	p.cursor = min(max(p.cursor, 0), len(runs)-1)
	return runs[p.cursor]
}

// panelAction is what a key on the panel asks of the UI around it.
type panelAction int

const (
	panelStay panelAction = iota
	panelClose
	// panelOpen asks for the selected run's response.
	panelOpen
)

// update handles a key, and says what the UI should do next.
func (p *runsPanel) update(msg tea.KeyPressMsg) panelAction {
	switch {
	case key.Matches(msg, p.keys.up):
		p.cursor--
	case key.Matches(msg, p.keys.down):
		p.cursor++
	case key.Matches(msg, p.keys.open):
		if e := p.selected(); e != nil {
			return panelOpen
		}
	case key.Matches(msg, p.keys.cancel):
		if e := p.selected(); e != nil && e.state != RunDone {
			p.s.runner.Cancel(e.id)
		}
	case key.Matches(msg, p.keys.close):
		return panelClose
	}
	p.selected()
	return panelStay
}

func (p *runsPanel) bindings() []key.Binding {
	return []key.Binding{p.keys.up, p.keys.open, p.keys.cancel, p.keys.close}
}

func (p *runsPanel) legend() legendSection {
	k := p.keys
	return legendSection{
		title:   "In the running commands panel (i)",
		compact: true,
		entries: []legendEntry{
			{of: k.up, desc: "select"},
			{of: k.open, desc: "open its response"},
			{of: k.cancel, desc: "cancel it"},
			{of: k.close, keys: "esc/i", desc: "close"},
		},
	}
}

func (p *runsPanel) equivalent() shellCommand {
	if e := p.selected(); e != nil {
		return e.display
	}
	return shellCommand{}
}

func (p *runsPanel) view(st styles, spin string, width, height int) string {
	runs := p.newestFirst()
	title := fmt.Sprintf("Commands — %d running", p.s.runs.inFlight())
	lines := []string{st.title.Render(title)}
	if len(runs) == 0 {
		lines = append(lines, st.dim.Render("Nothing has run yet."))
	}

	// The panel's border and padding take four columns and two rows.
	inner := width - 4
	rows := max(height-3, 1)
	p.selected()
	first := max(0, p.cursor-rows+1)
	for i := first; i < len(runs) && i < first+rows; i++ {
		lines = append(lines, p.row(st, runs[i], spin, i == p.cursor, inner))
	}
	return st.panel.Render(strings.Join(lines, "\n"))
}

func (p *runsPanel) row(st styles, e *runEntry, spin string, selected bool, width int) string {
	now := p.s.now()
	var state string
	switch {
	case e.state == RunQueued:
		state = st.dim.Render("queued " + elapsed(now.Sub(e.queued)))
	case e.state == RunRunning && e.asking:
		state = st.warnText.Render("waiting for your answer")
	case e.state == RunRunning:
		state = spin + " running " + elapsed(now.Sub(e.started))
	case e.state == RunDone:
		took := elapsed(e.result.Duration)
		if e.result.ExitCode == exitcode.OK {
			state = st.okText.Render("ok") + " " + took
		} else {
			state = st.errorText.Render(fmt.Sprintf("exit %d %s", e.result.ExitCode,
				exitcode.Meaning(e.result.ExitCode))) + " " + took
		}
	}

	marker := "  "
	if selected {
		marker = "> "
	}
	line := fmt.Sprintf("%s#%-3d %s  %s", marker, e.id, output.SanitizeCell(e.display.String()), state)
	if selected {
		line = st.selected.Render(line)
	}
	return clip(line, width)
}

// elapsed renders a duration as briefly as its size allows.
func elapsed(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}

// clip cuts s to width terminal cells. A width of zero or less means the width is
// not known yet, and s is left alone.
func clip(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}
