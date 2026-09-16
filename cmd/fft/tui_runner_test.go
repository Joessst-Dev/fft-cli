package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// runTimeout bounds how long a spec waits for a run to report. It is generous
// because -race slows every request down, and it is only ever reached on a failure.
const runTimeout = 10 * time.Second

// newRunner is a runner over the spec's Deps, shut down when the spec ends.
func (c *cli) newRunner() *cliRunner {
	r := newCLIRunner(context.Background(), c.deps)
	DeferCleanup(r.Close)
	return r
}

// awaitDone reads r's events until every run in ids has finished, and returns
// their results.
func awaitDone(r tui.Runner, ids ...tui.RunID) map[tui.RunID]tui.Result {
	GinkgoHelper()

	want := make(map[tui.RunID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}

	done := make(map[tui.RunID]tui.Result, len(ids))
	for len(done) < len(ids) {
		var ev tui.RunEvent
		Eventually(r.Events()).WithTimeout(runTimeout).Should(Receive(&ev))
		if ev.State == tui.RunDone && want[ev.ID] {
			done[ev.ID] = ev.Result
		}
	}
	return done
}

// start starts one invocation and fails the spec if the runner refuses it.
func start(r tui.Runner, inv tui.Invocation) tui.RunID {
	GinkgoHelper()
	id, err := r.Start(inv)
	Expect(err).NotTo(HaveOccurred())
	return id
}

var _ = Describe("the TUI's command runner", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("sends a generated operation with its body on stdin, and returns the API's JSON", func() {
		t := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"pj-1","status":"OPEN"}`))
		})
		r := c.newRunner()

		id := start(r, tui.Invocation{
			Args:  []string{"picking", "add-pick-job", "--file", "-"},
			Stdin: []byte(`{"pickLineItems":[]}`),
		})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		Expect(res.Status).To(Equal(http.StatusOK))
		Expect(res.Duration).To(BeNumerically(">", 0))

		sent := t.only()
		Expect(sent.Method).To(Equal(http.MethodPost))
		Expect(sent.json()).To(HaveKey("pickLineItems"))

		// -o json was added for the UI, so stdout is the API's document.
		var doc map[string]any
		Expect(json.Unmarshal(res.Stdout, &doc)).To(Succeed(), "stdout was not JSON: %s", res.Stdout)
		Expect(doc).To(HaveKeyWithValue("id", "pj-1"))
	})

	It("refuses a write against a read-only project without sending anything", func() {
		t := c.readOnlyProject(true)
		r := c.newRunner()
		r.SetProject("prod")

		id := start(r, tui.Invocation{
			Args:  []string{"picking", "add-pick-job", "--file", "-"},
			Stdin: []byte(`{"pickLineItems":[]}`),
		})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.ReadOnly))
		Expect(res.Status).To(BeZero(), "no response can have arrived")
		Expect(string(res.Stderr)).To(ContainSubstring(`project "prod" is read-only`))
		Expect(t.recorded()).To(BeEmpty(), "a read-only project was sent a request")
	})

	It("ends a cancelled run as an interrupted command would", func() {
		arrived := make(chan struct{}, 1)
		c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			arrived <- struct{}{}
			// Held until fft gives up on it, which is the thing under test.
			<-r.Context().Done()
		})
		r := c.newRunner()

		id := start(r, tui.Invocation{Args: []string{"picking", "get-pick-job", "--pick-job-id", "pj-1"}})
		Eventually(arrived).WithTimeout(runTimeout).Should(Receive())

		r.Cancel(id)

		Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.Interrupted))
	})

	It("refuses a component's command, which would take over the terminal", func() {
		c.installFake(fakeManifest("hello"))
		r := c.newRunner()

		id := start(r, tui.Invocation{Args: []string{"hello"}})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.Usage))
		Expect(string(res.Stderr)).To(ContainSubstring("hello component"))
		Expect(res.Stdout).To(BeEmpty(), "the component ran anyway")
	})

	It("runs commands side by side, never more than its slots at once", func() {
		var inFlight, peak atomic.Int32
		release := make(chan struct{})

		t := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			<-release

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"pj-1"}`))
		})
		// Registered after the tenant, so it runs first: a handler still parked on
		// release would otherwise hold the server's Close forever.
		releaseAll := sync.OnceFunc(func() { close(release) })
		DeferCleanup(releaseAll)

		r := c.newRunner()

		const runs = 3 * runnerSlots
		ids := make([]tui.RunID, 0, runs)
		for range runs {
			ids = append(ids, start(r, tui.Invocation{
				Args: []string{"picking", "get-pick-job", "--pick-job-id", "pj-1"},
			}))
		}

		// The runs arrive at the tenant and pile up there, up to the bound and no
		// further: the rest wait for a slot.
		Eventually(inFlight.Load).WithTimeout(runTimeout).Should(BeEquivalentTo(runnerSlots))
		Consistently(inFlight.Load).WithTimeout(100 * time.Millisecond).Should(BeEquivalentTo(runnerSlots))

		releaseAll()

		for id, res := range awaitDone(r, ids...) {
			Expect(res.ExitCode).To(Equal(exitcode.OK), "run %d: %s", id, res.Stderr)
		}
		Expect(t.recorded()).To(HaveLen(runs))
		Expect(peak.Load()).To(BeEquivalentTo(runnerSlots))
	})

	It("refuses to start anything once it has been shut down", func() {
		r := c.newRunner()
		r.Close()

		_, err := r.Start(tui.Invocation{Args: []string{"version"}})
		Expect(err).To(MatchError(errRunnerClosed))
		Expect(r.Events()).To(BeClosed())
	})

	Describe("the command line it runs", func() {
		var r *cliRunner

		BeforeEach(func() {
			r = c.newRunner()
		})

		DescribeTable("adds the flags the UI depends on",
			func(project string, args, want []string) {
				r.SetProject(project)
				Expect(r.argv(args)).To(Equal(want))
			},
			Entry("with no project selected",
				"", []string{"facility", "list"},
				[]string{"facility", "list", "-o", "json", "--no-color"}),
			Entry("with a project selected",
				"prod", []string{"facility", "list"},
				[]string{"facility", "list", "-o", "json", "--no-color", "--project", "prod"}),
			Entry("unless the command names its own project",
				"prod", []string{"facility", "list", "--project", "staging"},
				[]string{"facility", "list", "--project", "staging", "-o", "json", "--no-color"}),
			Entry("unless it names it with an equals sign",
				"prod", []string{"facility", "list", "--project=staging"},
				[]string{"facility", "list", "--project=staging", "-o", "json", "--no-color"}),
			Entry("before a --, after which they would be arguments",
				"prod", []string{"api", "getFacility", "--", "--project"},
				[]string{"api", "getFacility", "-o", "json", "--no-color", "--project", "prod", "--", "--project"}),
		)

		It("does not let the caller rewrite a queued command through its own slice", func() {
			args := []string{"version"}
			id := start(r, tui.Invocation{Args: args})
			args[0] = "nonsense"

			Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))
		})
	})
})

// runDepsClassification is every Deps field, and what forRun does with it. A field
// added to Deps and not here fails the spec below, which is the prompt to decide
// whether a concurrent TUI run may share it.
var runDepsClassification = map[string]string{
	"Config":         "shared",
	"Secrets":        "shared",
	"Clock":          "shared",
	"Verify":         "shared",
	"NewTokenSource": "shared",
	"NewInstaller":   "shared",
	"Retry":          "shared",
	"Update":         "shared",
	"unused":         "shared",
	"Components":     "shared",

	"In":       "per run",
	"Terminal": "per run",

	"Printer":             "rebuilt",
	"Prompt":              "rebuilt",
	"Debug":               "rebuilt",
	"Project":             "rebuilt",
	"Ephemeral":           "rebuilt",
	"Timeout":             "rebuilt",
	"AssumeYes":           "rebuilt",
	"ReadOnlyFlag":        "rebuilt",
	"ReadOnlyEnv":         "rebuilt",
	"noKeyringFromConfig": "rebuilt",
	"explicitNoKeyring":   "rebuilt",
	"cfg":                 "rebuilt",
	"componentWarnings":   "rebuilt",
	"updates":             "rebuilt",
	"updateState":         "rebuilt",
	"updateDone":          "rebuilt",
	"StartTUI":            "zero",
	"observeStatus":       "caller",
}

var _ = Describe("a Deps for one TUI run", func() {
	It("has every field classified", func() {
		deps := reflect.TypeFor[Deps]()
		for i := range deps.NumField() {
			f := deps.Field(i)
			Expect(runDepsClassification).To(HaveKey(f.Name),
				"Deps.%s is new: decide in Deps.forRun whether a concurrent run shares it, sets it, or rebuilds it, "+
					"and record the decision in runDepsClassification", f.Name)
		}
	})

	It("carries the shared fields over, and nothing it must rebuild", func() {
		parent := newCLI().deps
		parent.Project = "prod"
		parent.cfg = config.New()
		parent.AssumeYes = true

		in := bytes.NewReader(nil)
		child := parent.forRun(in)

		Expect(child.Config).To(BeIdenticalTo(parent.Config))
		Expect(child.Secrets).To(BeIdenticalTo(parent.Secrets))
		Expect(child.In).To(BeIdenticalTo(in))
		Expect(child.Terminal).To(HaveValue(BeFalse()))

		Expect(child.Project).To(BeEmpty())
		Expect(child.cfg).To(BeNil())
		Expect(child.AssumeYes).To(BeFalse())
	})
})
