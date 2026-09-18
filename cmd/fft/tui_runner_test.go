package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	r := newCLIRunner(context.Background(), c.deps, uiRun{})
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

// awaitState reads r's events until run id enters state.
func awaitState(r tui.Runner, id tui.RunID, state tui.RunState) {
	GinkgoHelper()
	for {
		var ev tui.RunEvent
		Eventually(r.Events()).WithTimeout(runTimeout).Should(Receive(&ev))
		if ev.ID == id && ev.State == state {
			return
		}
	}
}

// configuredTenant is [cli.fakeTenant] reached through the config file rather than
// the environment: two projects, "prod" (the active one) and "other", both pointing
// at it. The runner's config-file handling can only be seen with a config file.
func (c *cli) configuredTenant(handle func(w http.ResponseWriter, r *http.Request)) *tenant {
	t := &tenant{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.record(call{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query()})
		handle(w, r)
	}))
	DeferCleanup(srv.Close)

	cfg := config.New()
	cfg.ActiveProject = "prod"
	for _, name := range []string{"prod", "other"} {
		cfg.Upsert(config.Project{
			Name:           name,
			BaseURL:        srv.URL + "/" + name,
			FirebaseAPIKey: "AIzaSyExample",
			Email:          "bot@ocff-acme-" + name + ".com",
		})
	}
	Expect(c.deps.Config.Save(cfg)).To(Succeed())
	return t
}

// activeProject is the project the config file on disk names as active.
func (c *cli) activeProject() string {
	GinkgoHelper()
	cfg, err := c.deps.Config.Load()
	Expect(err).NotTo(HaveOccurred())
	return cfg.ActiveProject
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

	It("runs a curated command whose table, read back from its JSON, is the one the shell prints", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
		})
		r := c.newRunner()

		id := start(r, tui.Invocation{Args: []string{"facility", "get", "BER-01"}})
		res := awaitDone(r, id)[id]
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)

		var get tui.Command
		for _, g := range r.Catalog().Groups() {
			for _, op := range g.Operations {
				if slices.Equal(op.Command.Path, []string{"facility", "get"}) {
					get = op.Command
				}
			}
		}
		Expect(get.Table).To(BeTrue())
		table, err := r.Catalog().Table(get, res.Stdout)
		Expect(err).NotTo(HaveOccurred())

		Expect(c.run("facility", "get", "BER-01")).To(Equal(exitcode.OK), c.errOut())
		Expect(table).To(Equal(c.out()))
		Expect(table).To(ContainSubstring("BER-01"))
	})

	It("sends a read two commands share through the curated one, as written, with the table the shell prints", func() {
		t := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, stockPage(fixture("stock.json"), fixture("stock_empty.json")))
		})
		r := c.newRunner()
		const body = `{"query":{"and":[{"tenantArticleId":{"eq":"4711"}},{"value":{"notEq":16777217}}]},"size":10}`

		search := catalogOps(r.Catalog())["searchStock"].Command
		Expect(search.Path).To(Equal([]string{"stock", "search"}))
		Expect(search.Table).To(BeTrue())

		id := start(r, tui.Invocation{Args: append(slices.Clone(search.Path), "--file", "-"), Stdin: []byte(body)})
		res := awaitDone(r, id)[id]
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)

		sent := t.only()
		Expect(sent.Method).To(Equal(http.MethodPost))
		Expect(sent.Path).To(Equal("/api/stocks/search"))
		Expect(string(sent.Body)).To(Equal(body))

		table, err := r.Catalog().Table(search, res.Stdout)
		Expect(err).NotTo(HaveOccurred())

		file := filepath.Join(GinkgoT().TempDir(), "query.json")
		Expect(os.WriteFile(file, []byte(body), 0o600)).To(Succeed())
		Expect(c.run("stock", "search", "--file", file)).To(Equal(exitcode.OK), c.errOut())
		Expect(table).To(Equal(c.out()))
		Expect(table).To(ContainSubstring("shelf-a-12"))
	})

	It("never puts what a run reads on stdin into an event", func() {
		const body = `{"pickLineItems":[],"note":"stdin-must-not-appear"}`
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"pj-1"}`))
		})
		r := c.newRunner()

		id := start(r, tui.Invocation{Args: []string{"picking", "add-pick-job", "--file", "-"}, Stdin: []byte(body)})

		var events []tui.RunEvent
		for {
			var ev tui.RunEvent
			Eventually(r.Events()).WithTimeout(runTimeout).Should(Receive(&ev))
			events = append(events, ev)
			if ev.ID == id && ev.State == tui.RunDone {
				break
			}
		}
		Expect(events).To(HaveLen(3))
		Expect(events[2].Result.ExitCode).To(Equal(exitcode.OK), "stderr: %s", events[2].Result.Stderr)
		for _, ev := range events {
			Expect(ev.Invocation.Stdin).To(BeNil(), "state %d", ev.State)
			Expect(fmt.Sprintf("%+v", ev)).NotTo(ContainSubstring("stdin-must-not-appear"))
		}
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

	It("does not run a cancelled command that was waiting for the config file", func() {
		arrived := make(chan struct{}, 1)
		release := make(chan struct{})
		c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
			arrived <- struct{}{}
			<-release
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"pj-1"}`))
		})
		releaseAll := sync.OnceFunc(func() { close(release) })
		DeferCleanup(releaseAll)
		r := c.newRunner()

		// A read holds the config file open for reading, so the switch below has to
		// wait for it.
		blocking := start(r, tui.Invocation{Args: []string{"picking", "get-pick-job", "--pick-job-id", "pj-1"}})
		Eventually(arrived).WithTimeout(runTimeout).Should(Receive())

		switching := start(r, tui.Invocation{Args: []string{"project", "use", "other"}})
		awaitState(r, switching, tui.RunRunning)
		r.Cancel(switching)
		releaseAll()

		res := awaitDone(r, blocking, switching)
		Expect(res[blocking].ExitCode).To(Equal(exitcode.OK), "stderr: %s", res[blocking].Stderr)
		Expect(res[switching].ExitCode).To(Equal(exitcode.Interrupted), "stderr: %s", res[switching].Stderr)
		Expect(c.activeProject()).To(Equal("prod"), "a cancelled switch went ahead")
	})

	It("refuses a component's command inside the run itself, whatever resolved it beforehand", func() {
		c.installFake(fakeManifest("hello"))
		var stdout, stderr bytes.Buffer

		code := execute(context.Background(), c.deps.forRun(bytes.NewReader(nil), uiRun{}),
			[]string{"hello"}, nil, &stdout, &stderr)

		Expect(code).To(Equal(exitcode.Usage))
		Expect(stderr.String()).To(ContainSubstring("hello component"))
		Expect(stdout.String()).To(BeEmpty(), "the component ran anyway")
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

	Describe("a command that must run alone", func() {
		var (
			r          *cliRunner
			arrived    chan struct{}
			releaseAll func()
			blocking   tui.RunID
		)

		BeforeEach(func() {
			arrived = make(chan struct{}, 1)
			release := make(chan struct{})
			c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
				arrived <- struct{}{}
				<-release
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"pj-1"}`))
			})
			releaseAll = sync.OnceFunc(func() { close(release) })
			DeferCleanup(releaseAll)

			r = c.newRunner()
			blocking = start(r, tui.Invocation{Args: []string{"picking", "get-pick-job", "--pick-job-id", "pj-1"}})
			Eventually(arrived).WithTimeout(runTimeout).Should(Receive())
		})

		DescribeTable("waits for every run in flight to finish",
			func(inv tui.Invocation) {
				alone := start(r, inv)
				awaitState(r, alone, tui.RunRunning)

				// A free slot is not enough: the run in flight still holds the config file.
				Consistently(r.Events()).WithTimeout(200 * time.Millisecond).ShouldNot(Receive())

				releaseAll()
				for id, res := range awaitDone(r, blocking, alone) {
					Expect(res.ExitCode).To(Equal(exitcode.OK), "run %d: %s", id, res.Stderr)
				}
			},
			Entry("when the caller asks for it", tui.Invocation{Args: []string{"version"}, Exclusive: true}),
			Entry("when the command rewrites the config file", tui.Invocation{Args: []string{"project", "use", "other"}}),
		)

		It("holds back the runs that start after it", func() {
			releaseAll()
			Expect(awaitDone(r, blocking)[blocking].ExitCode).To(Equal(exitcode.OK))

			// Now the exclusive run is the one in flight, held by the tenant in turn.
			second := make(chan struct{})
			c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
				arrived <- struct{}{}
				<-second
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"pj-1"}`))
			})
			releaseSecond := sync.OnceFunc(func() { close(second) })
			DeferCleanup(releaseSecond)

			alone := start(r, tui.Invocation{
				Args:      []string{"picking", "get-pick-job", "--pick-job-id", "pj-1"},
				Exclusive: true,
			})
			Eventually(arrived).WithTimeout(runTimeout).Should(Receive())

			after := start(r, tui.Invocation{Args: []string{"version"}})
			awaitState(r, after, tui.RunRunning)
			Consistently(r.Events()).WithTimeout(200 * time.Millisecond).ShouldNot(Receive())

			releaseSecond()
			for id, res := range awaitDone(r, alone, after) {
				Expect(res.ExitCode).To(Equal(exitcode.OK), "run %d: %s", id, res.Stderr)
			}
		})
	})

	When("every slot is taken", func() {
		var (
			r          *cliRunner
			t          *tenant
			held       []tui.RunID
			releaseAll func()
		)

		BeforeEach(func() {
			arrived := make(chan struct{}, runnerSlots)
			release := make(chan struct{})
			t = c.configuredTenant(func(w http.ResponseWriter, req *http.Request) {
				if strings.HasSuffix(req.URL.Path, "/HOLD") {
					arrived <- struct{}{}
					<-release
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"pj-1"}`))
			})
			releaseAll = sync.OnceFunc(func() { close(release) })
			DeferCleanup(releaseAll)

			r = c.newRunner()
			r.SetProject("prod")
			held = nil
			for range runnerSlots {
				held = append(held, start(r, tui.Invocation{Args: []string{"picking", "get-pick-job", "--pick-job-id", "HOLD"}}))
			}
			for range runnerSlots {
				Eventually(arrived).WithTimeout(runTimeout).Should(Receive())
			}
		})

		It("sends a queued run to the project selected when it was started, not when its turn came", func() {
			queued := start(r, tui.Invocation{Args: []string{"picking", "get-pick-job", "--pick-job-id", "QUEUED"}})
			r.SetProject("other")
			releaseAll()

			res := awaitDone(r, append(held, queued)...)
			Expect(res[queued].ExitCode).To(Equal(exitcode.OK), "stderr: %s", res[queued].Stderr)
			Expect(t.recorded()).To(ContainElement(HaveField("Path", "/prod/api/pickjobs/QUEUED")))
			Expect(t.recorded()).NotTo(ContainElement(HaveField("Path", "/other/api/pickjobs/QUEUED")))
		})

		It("runs the commands that must run alone in the order they were started", func() {
			var switches []tui.RunID
			for _, name := range []string{"other", "prod", "other", "prod", "other", "prod", "other"} {
				switches = append(switches, start(r, tui.Invocation{Args: []string{"project", "use", name}}))
			}
			releaseAll()

			var ran []tui.RunID
			finished := map[tui.RunID]bool{}
			for len(finished) < len(held)+len(switches) {
				var ev tui.RunEvent
				Eventually(r.Events()).WithTimeout(runTimeout).Should(Receive(&ev))
				switch {
				case ev.State == tui.RunRunning && slices.Contains(switches, ev.ID):
					Expect(ev.Invocation.Exclusive).To(BeTrue(), "project use rewrites the config file")
					ran = append(ran, ev.ID)
				case ev.State == tui.RunDone:
					Expect(ev.Result.ExitCode).To(Equal(exitcode.OK), "run %d: %s", ev.ID, ev.Result.Stderr)
					finished[ev.ID] = true
				}
			}
			Expect(ran).To(Equal(switches))
			Expect(c.activeProject()).To(Equal("other"), "the last switch did not win")
		})

		It("keeps the line moving past an exclusive run cancelled while it waited", func() {
			first := start(r, tui.Invocation{Args: []string{"project", "use", "prod"}})
			second := start(r, tui.Invocation{Args: []string{"version"}, Exclusive: true})
			third := start(r, tui.Invocation{Args: []string{"project", "use", "other"}})
			r.Cancel(first)
			releaseAll()

			res := awaitDone(r, append(held, first, second, third)...)
			Expect(res[first].ExitCode).To(Equal(exitcode.Interrupted))
			Expect(res[second].ExitCode).To(Equal(exitcode.OK), "stderr: %s", res[second].Stderr)
			Expect(res[third].ExitCode).To(Equal(exitcode.OK), "stderr: %s", res[third].Stderr)
			Expect(c.activeProject()).To(Equal("other"))
		})
	})

	It("ends a run cancelled while it waited for a slot, without sending it", func() {
		arrived := make(chan struct{}, runnerSlots)
		release := make(chan struct{})
		t := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			arrived <- struct{}{}
			<-release
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"pj-1"}`))
		})
		releaseAll := sync.OnceFunc(func() { close(release) })
		DeferCleanup(releaseAll)
		r := c.newRunner()

		held := make([]tui.RunID, 0, runnerSlots)
		for range runnerSlots {
			held = append(held, start(r, tui.Invocation{Args: []string{"picking", "get-pick-job", "--pick-job-id", "pj-1"}}))
		}
		for range runnerSlots {
			Eventually(arrived).WithTimeout(runTimeout).Should(Receive())
		}

		queued := start(r, tui.Invocation{Args: []string{"picking", "add-pick-job", "--data", `{"pickLineItems":[]}`}})
		awaitState(r, queued, tui.RunQueued)
		r.Cancel(queued)

		// It ends while every slot is still taken: it never needed one to stop.
		Expect(awaitDone(r, queued)[queued].ExitCode).To(Equal(exitcode.Interrupted))

		releaseAll()
		awaitDone(r, held...)
		Expect(t.recorded()).To(HaveLen(runnerSlots), "the cancelled run was sent")
	})

	It("shuts down while its runs are blocked on an events buffer nobody reads", func() {
		r := newCLIRunner(context.Background(), c.deps, uiRun{})

		// Three events a run, and more runs than the buffer has room for.
		for range runnerEventBuffer {
			start(r, tui.Invocation{Args: []string{"version"}})
		}
		Eventually(func() int { return len(r.events) }).WithTimeout(runTimeout).Should(Equal(runnerEventBuffer))

		closed := make(chan struct{})
		go func() {
			defer close(closed)
			r.Close()
		}()
		Eventually(closed).WithTimeout(runTimeout).Should(BeClosed())

		// Whatever was buffered is still delivered, and then the stream ends.
		Eventually(r.Events()).WithTimeout(runTimeout).Should(BeClosed())
	})

	It("refuses a write in a session started read-only", func() {
		t := c.readOnlyProject(false)
		r := newCLIRunner(context.Background(), c.deps, uiRun{readOnly: true})
		DeferCleanup(r.Close)

		id := start(r, tui.Invocation{Args: []string{"picking", "add-pick-job", "--data", `{"pickLineItems":[]}`}})
		res := awaitDone(r, id)[id]

		Expect(res.ExitCode).To(Equal(exitcode.ReadOnly), "stderr: %s", res.Stderr)
		Expect(t.recorded()).To(BeEmpty())
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

		It("answers in the API's JSON, whatever the config file prefers", func() {
			t := c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"facilities":[{"id":"f-1","name":"Berlin"}],"total":1}`))
			})
			cfg, err := c.deps.Config.Load()
			Expect(err).NotTo(HaveOccurred())
			cfg.Settings.Output = "table"
			Expect(c.deps.Config.Save(cfg)).To(Succeed())

			id := start(r, tui.Invocation{Args: []string{"facility", "list"}})
			res := awaitDone(r, id)[id]

			Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
			Expect(t.recorded()).To(HaveLen(1))
			Expect(json.Valid(res.Stdout)).To(BeTrue(), "stdout was not JSON: %s", res.Stdout)
		})

		It("acts on the project the UI selected", func() {
			t := c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"facilities":[],"total":0}`))
			})
			r.SetProject("other")

			id := start(r, tui.Invocation{Args: []string{"facility", "list"}})
			res := awaitDone(r, id)[id]

			Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
			Expect(t.recorded()).To(ConsistOf(HaveField("Path", HavePrefix("/other/"))))
		})

		It("sends an invocation pinned to a project there, whichever project is selected", func() {
			t := c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
				answerJSON(w, emptyFacilities)
			})
			r.SetProject("other")

			id := start(r, tui.Invocation{Args: []string{"facility", "list"}, Project: "prod"})
			res := awaitDone(r, id)[id]

			Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
			Expect(t.recorded()).To(ConsistOf(HaveField("Path", HavePrefix("/prod/"))))
			Expect(res.Project).To(Equal("prod"))
		})

		It("reports the project a run acted on, even when fft's own resolution chose it", func() {
			c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
				answerJSON(w, emptyFacilities)
			})

			id := start(r, tui.Invocation{Args: []string{"facility", "list"}})
			Expect(awaitDone(r, id)[id].Project).To(Equal("prod"))
		})

		It("reports no project for a run that never chose one", func() {
			c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
				answerJSON(w, emptyFacilities)
			})

			id := start(r, tui.Invocation{Args: []string{"facility", "list", "--no-such-flag"}})
			res := awaitDone(r, id)[id]

			Expect(res.ExitCode).To(Equal(exitcode.Usage))
			Expect(res.Project).To(BeEmpty())
		})

		It("keeps only the first part of an answer too large to hold, and says so", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				answerJSON(w, `{"id":"pj-1","note":"`+strings.Repeat("x", 4096)+`"}`)
			})
			r.stdoutLimit = 1024

			id := start(r, tui.Invocation{Args: []string{"picking", "get-pick-job", "--pick-job-id", "pj-1"}})
			res := awaitDone(r, id)[id]

			Expect(res.ExitCode).To(Equal(exitcode.OK), "the command must finish however much the UI keeps")
			Expect(res.Stdout).To(HaveLen(1024))
			Expect(res.StdoutTruncated).To(BeTrue())
			Expect(res.StderrTruncated).To(BeFalse())
		})

		It("keeps the whole of an answer that fits", func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				answerJSON(w, `{"id":"pj-1"}`)
			})

			id := start(r, tui.Invocation{Args: []string{"picking", "get-pick-job", "--pick-job-id", "pj-1"}})
			res := awaitDone(r, id)[id]

			Expect(json.Valid(res.Stdout)).To(BeTrue())
			Expect(res.StdoutTruncated).To(BeFalse())
		})

		// A --project that is really the value of the flag before it used to hide the
		// selection from a runner that looked for the flag by name, and the run then
		// went to the active project instead of the selected one.
		It("acts on the selected project when --project is only another flag's value", func() {
			t := c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"facilities":[],"total":0}`))
			})
			r.SetProject("other")

			id := start(r, tui.Invocation{Args: []string{"facility", "list", "--tenant-facility-id", "--project"}})
			res := awaitDone(r, id)[id]

			Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
			Expect(t.recorded()).To(ConsistOf(HaveField("Path", HavePrefix("/other/"))))
		})

		DescribeTable("refuses a global flag the UI decides, sending nothing",
			func(flag ...string) {
				t := c.configuredTenant(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				})
				r.SetProject("other")

				id := start(r, tui.Invocation{Args: append([]string{"facility", "list"}, flag...)})
				res := awaitDone(r, id)[id]

				Expect(res.ExitCode).To(Equal(exitcode.Usage), "stderr: %s", res.Stderr)
				Expect(string(res.Stderr)).To(ContainSubstring("fft tui"))
				Expect(t.recorded()).To(BeEmpty())
			},
			Entry("--project", "--project", "prod"),
			Entry("--project=", "--project=prod"),
			Entry("-o", "-o", "table"),
			Entry("--no-keyring", "--no-keyring"),
		)

		// A value-taking flag at the end of a command line used to swallow the first
		// flag the runner appended, and the one after that became the command: with a
		// component named after an output format, `--timeout` ran it.
		It("does not run a component a dangling flag would have led to", func() {
			c.installFake(fakeManifest("json"))

			id := start(r, tui.Invocation{Args: []string{"--timeout"}})
			res := awaitDone(r, id)[id]

			Expect(res.ExitCode).To(Equal(exitcode.Usage), "stderr: %s", res.Stderr)
			Expect(res.Stdout).To(BeEmpty(), "the component ran")
		})

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
	"tokens":              "caller",
	"Prompt":              "caller",
	"HistoryPath":         "shared",
	"historyMaxBytes":     "shared",
	"run":                 "rebuilt",
	"ui":                  "per run",
}

var _ = Describe("the buffer a TUI run's output is kept in", func() {
	It("keeps the first bytes up to its limit, and says it dropped the rest", func() {
		b := cappedBuffer{limit: 10}
		for _, chunk := range []string{"abc", "defghij", "klm"} {
			n, err := b.Write([]byte(chunk))
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(len(chunk)))
		}
		Expect(string(b.buf)).To(Equal("abcdefghij"))
		Expect(b.dropped).To(BeTrue())
	})

	It("never reserves more than its limit, however it is written to", func() {
		const limit = 1000
		b := cappedBuffer{limit: limit}
		for range 300 {
			_, _ = b.Write([]byte("0123456"))
			Expect(cap(b.buf)).To(BeNumerically("<=", limit))
		}
		Expect(b.buf).To(HaveLen(limit))
	})

	It("keeps nothing, and reserves nothing, with no room at all", func() {
		b := cappedBuffer{}
		_, _ = b.Write([]byte("x"))
		Expect(b.buf).To(BeEmpty())
		Expect(cap(b.buf)).To(BeZero())
		Expect(b.dropped).To(BeTrue())
	})
})

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
		child := parent.forRun(in, uiRun{readOnly: true})

		Expect(child.Config).To(BeIdenticalTo(parent.Config))
		Expect(child.Secrets).To(BeIdenticalTo(parent.Secrets))
		Expect(child.In).To(BeIdenticalTo(in))
		Expect(child.Terminal).To(HaveValue(BeFalse()))
		Expect(child.ui).To(HaveValue(Equal(uiRun{readOnly: true})))

		Expect(child.Project).To(BeEmpty())
		Expect(child.cfg).To(BeNil())
		Expect(child.AssumeYes).To(BeFalse())
	})
})
