package tui

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// scriptedRunner finishes every run on its own, the way the real runner does, and
// answers as a tenant with two projects would. It is safe for the program's
// goroutines.
type scriptedRunner struct {
	mu       sync.Mutex
	events   chan RunEvent
	next     RunID
	log      []string
	selected []string
	active   string
	hold     map[string]bool
}

func newScriptedRunner() *scriptedRunner {
	return &scriptedRunner{events: make(chan RunEvent, 64), active: "staging", hold: map[string]bool{}}
}

func (r *scriptedRunner) Start(inv Invocation) (RunID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	id := r.next
	line := strings.Join(inv.Args, " ")
	r.log = append(r.log, line)

	res := r.answer(inv.Args)
	held := r.hold[line]
	go func() {
		r.events <- RunEvent{ID: id, State: RunQueued, Invocation: inv, At: time.Now()}
		r.events <- RunEvent{ID: id, State: RunRunning, Invocation: inv, At: time.Now()}
		if !held {
			r.events <- RunEvent{ID: id, State: RunDone, Invocation: inv, At: time.Now(), Result: res}
		}
	}()
	return id, nil
}

// answer is what the command would print. The caller holds mu.
func (r *scriptedRunner) answer(args []string) Result {
	switch strings.Join(args[:2], " ") {
	case "project list":
		return ok(strings.NewReplacer("STAGING", boolJSON(r.active == "staging"), "PROD", boolJSON(r.active == "prod")).Replace(`[
		  {"name":"staging","active":STAGING,"baseUrl":"https://staging.example.com","credential":"keyring"},
		  {"name":"prod","active":PROD,"baseUrl":"https://prod.example.com","credential":"keyring","readOnly":true}
		]`))
	case "auth status":
		project := r.active
		if n := len(r.selected); n > 0 && r.selected[n-1] != "" {
			project = r.selected[n-1]
		}
		return ok(`{"project":"` + project + `","store":"keyring","signIn":"password","token":"valid","expiresAt":"2026-07-12T13:00:00Z"}`)
	case "project use":
		r.active = args[2]
	case "picking get-pick-job":
		res := ok(`{"id":"pj-1","status":"OPEN"}`)
		res.Status = 200
		return res
	}
	return ok(`{}`)
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (r *scriptedRunner) Cancel(RunID)               {}
func (r *scriptedRunner) Answer(RunID, uint64, bool) {}
func (r *scriptedRunner) Events() <-chan RunEvent    { return r.events }

func (r *scriptedRunner) SetProject(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selected = append(r.selected, name)
}

func (r *scriptedRunner) commandLines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.log)
}

func (r *scriptedRunner) projects() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.selected)
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// snapshot is the model's state, as read on the program's own event loop.
type snapshot struct {
	view     string
	inFlight int
}

// snapshotRequest asks a [probed] model for a snapshot.
type snapshotRequest chan snapshot

// probed answers snapshot requests from inside Update, which is the one place the
// model may be read while the program runs.
type probed struct{ *app }

func (p probed) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if req, isReq := msg.(snapshotRequest); isReq {
		req <- snapshot{view: ansi.Strip(p.app.View().Content), inFlight: p.s.runs.inFlight()}
		return p, nil
	}
	_, cmd := p.app.Update(msg)
	return p, cmd
}

// These run the UI as a real Bubble Tea program — its event loop, its renderer,
// the runner's event channel — and drive it only by sending it keys. What the
// program ends with is read from the model Run returns, once nothing else touches
// it.
var _ = Describe("the UI as a running program", func() {
	var (
		r     *scriptedRunner
		out   *syncBuffer
		p     *tea.Program
		ended chan tea.Model
	)

	BeforeEach(func() {
		r = newScriptedRunner()
		out = &syncBuffer{}
	})

	run := func() {
		now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
		p = tea.NewProgram(probed{newApp(Options{Runner: r, Catalog: fakeCatalog{}, Now: func() time.Time { return now }})},
			tea.WithContext(context.Background()),
			tea.WithInput(nil),
			tea.WithOutput(out),
			tea.WithWindowSize(120, 30),
			tea.WithoutSignals(),
		)
		ended = make(chan tea.Model, 1)
		go func() {
			defer GinkgoRecover()
			m, err := p.Run()
			// Killed is how cleanup ends a program a failed spec left running.
			if !errors.Is(err, tea.ErrProgramKilled) {
				Expect(err).NotTo(HaveOccurred())
			}
			ended <- m
		}()
		DeferCleanup(func() {
			p.Kill()
			Eventually(ended).Should(Receive())
		})
	}

	finalView := func() string {
		GinkgoHelper()
		var m tea.Model
		Eventually(ended).WithTimeout(5 * time.Second).Should(Receive(&m))
		ended <- m
		return ansi.Strip(m.View().Content)
	}

	current := func() snapshot {
		req := make(snapshotRequest, 1)
		p.Send(req)
		select {
		case snap := <-req:
			return snap
		case <-time.After(time.Second):
			return snapshot{inFlight: -1}
		}
	}
	view := func() string { return current().view }
	inFlight := func() int { return current().inFlight }

	// loaded waits until the list is on screen, so that a key sent next has rows to
	// act on.
	loaded := func() {
		GinkgoHelper()
		Eventually(view).Should(ContainSubstring("prod.example.com"))
	}

	It("switches project: the switch, then a sign-in on its own, then the new state", func() {
		run()
		Eventually(r.commandLines).Should(Equal([]string{"project list", "auth status"}))
		Eventually(out.String).Should(ContainSubstring("\x1b[?1049h"), "the UI is not on the alternate screen")
		loaded()

		p.Send(keyPress("down"))
		p.Send(keyPress("enter"))

		Eventually(r.projects).Should(Equal([]string{"prod"}))
		Eventually(r.commandLines).Should(HaveLen(6))
		lines := r.commandLines()
		Expect(lines[2]).To(Equal("project use prod"))
		Expect(lines[3:5]).To(ConsistOf("project list", "auth whoami"))
		Expect(lines[5]).To(Equal("auth status"))
		Eventually(inFlight).Should(BeZero())

		p.Send(keyPress("q"))
		final := finalView()
		Expect(final).To(ContainSubstring("Now using prod."))
		Expect(final).To(ContainSubstring("fft · prod · RO · token 1h00m left"))
		Expect(final).To(MatchRegexp(`\* prod`))
	})

	It("finds an operation, sends it, and shows what came back", func() {
		run()
		loaded()

		p.Send(keyPress("2"))
		p.Send(keyPress("/"))
		for _, r := range "getPick" {
			p.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		// The search answers in the background; enter applies it once it has.
		Eventually(view).Should(ContainSubstring("1 operation"))
		p.Send(keyPress("enter"))
		p.Send(keyPress("enter"))
		Eventually(view).Should(ContainSubstring("--pick-job-id (required)"))

		p.Send(keyPress("enter"))
		for _, r := range "pj-1" {
			p.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		p.Send(keyPress("enter"))
		p.Send(keyPress("s"))

		Eventually(r.commandLines).Should(ContainElement("picking get-pick-job --pick-job-id pj-1"))
		Eventually(view).Should(ContainSubstring(`"status": "OPEN"`))
		Expect(view()).To(ContainSubstring("exit 0 (success) · HTTP 200"))
		Expect(view()).To(ContainSubstring("[4 Response]"))
	})

	It("asks before quitting while a command runs, and quits on yes", func() {
		r.hold["project use prod"] = true
		run()
		loaded()

		p.Send(keyPress("down"))
		p.Send(keyPress("enter"))
		Eventually(r.commandLines).Should(HaveLen(3))

		Eventually(inFlight).Should(Equal(1))
		p.Send(keyPress("q"))
		Eventually(view).Should(ContainSubstring("1 command is still running"))
		Expect(ended).NotTo(Receive(), "quit without asking")

		p.Send(keyPress("y"))
		Expect(finalView()).To(ContainSubstring("1 command is still running"))
	})
})
