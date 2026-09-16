package main

import (
	"context"
	"errors"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

var _ = Describe("fft tui", func() {
	var (
		c       *cli
		t       *tenant
		started []tui.Options
	)

	BeforeEach(func() {
		c = newCLI()
		t = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.WriteHeader(http.StatusInternalServerError)
		})

		started = nil
		c.deps.StartTUI = func(_ context.Context, opts tui.Options) error {
			started = append(started, opts)
			return nil
		}
	})

	When("there is no terminal to draw on", func() {
		BeforeEach(func() {
			c.deps.Terminal = ptr(false)
		})

		It("refuses as a usage error, sending nothing and printing no data", func() {
			Expect(c.run("tui")).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring("needs a terminal"))
			Expect(c.out()).To(BeEmpty())
			Expect(t.recorded()).To(BeEmpty())
			Expect(started).To(BeEmpty(), "the UI was started without a terminal")
		})
	})

	When("it is at a terminal", func() {
		BeforeEach(func() {
			c.deps.Terminal = ptr(true)
		})

		It("hands the UI a runner and the stderr stream to draw on", func() {
			Expect(c.run("tui")).To(Equal(exitcode.OK))

			Expect(started).To(HaveLen(1))
			Expect(started[0].Runner).NotTo(BeNil())
			Expect(started[0].Out).To(BeIdenticalTo(&c.stderr))
			Expect(started[0].In).To(BeIdenticalTo(c.stdin))
			Expect(c.out()).To(BeEmpty())
		})

		It("gives the UI a runner that is shut down once the UI returns", func() {
			Expect(c.run("tui")).To(Equal(exitcode.OK))

			_, err := started[0].Runner.Start(tui.Invocation{Args: []string{"version"}})
			Expect(err).To(MatchError(errRunnerClosed))
		})

		It("reports the UI's failure with the exit code it means", func() {
			c.deps.StartTUI = func(context.Context, tui.Options) error {
				return context.Canceled
			}
			Expect(c.run("tui")).To(Equal(exitcode.Interrupted))

			c.deps.StartTUI = func(context.Context, tui.Options) error {
				return errors.New("the terminal went away")
			}
			Expect(c.run("tui")).To(Equal(exitcode.General))
			Expect(c.errOut()).To(ContainSubstring("the terminal went away"))
		})

		It("takes no arguments", func() {
			Expect(c.run("tui", "facility")).To(Equal(exitcode.Usage))
			Expect(started).To(BeEmpty())
		})
	})
})

// A flag given to `fft tui` is a flag for the whole session. Each run builds its
// own Deps and parses only its own command line, so a session flag that is not
// carried over explicitly is a flag the runs never hear of — and for --read-only
// that is a guardrail the user asked for and did not get.
var _ = Describe("fft tui's session flags", func() {
	var (
		c       *cli
		t       *tenant
		results []tui.Result
	)

	// session starts the UI with flags, and has it run each of invocations to the
	// end before it returns.
	session := func(flags []string, invocations ...tui.Invocation) int {
		GinkgoHelper()
		c.deps.StartTUI = func(_ context.Context, opts tui.Options) error {
			for _, inv := range invocations {
				id := start(opts.Runner, inv)
				results = append(results, awaitDone(opts.Runner, id)[id])
			}
			return nil
		}
		return c.run(append([]string{"tui"}, flags...)...)
	}

	pickJob := func(extra ...string) tui.Invocation {
		return tui.Invocation{
			Args:  append([]string{"picking", "add-pick-job", "--file", "-"}, extra...),
			Stdin: []byte(`{"pickLineItems":[]}`),
		}
	}

	BeforeEach(func() {
		c = newCLI()
		c.deps.Terminal = ptr(true)
		t = c.readOnlyProject(false)
		results = nil
	})

	When("the session was started with --read-only", func() {
		It("refuses a write, sending nothing", func() {
			Expect(session([]string{"--read-only"}, pickJob())).To(Equal(exitcode.OK))

			Expect(results).To(HaveLen(1))
			Expect(results[0].ExitCode).To(Equal(exitcode.ReadOnly), "stderr: %s", results[0].Stderr)
			Expect(string(results[0].Stderr)).To(ContainSubstring("fft tui"))
			Expect(t.recorded()).To(BeEmpty(), "a write was sent in a read-only session")
		})

		It("refuses a run's own --read-only=false rather than letting it loosen the session", func() {
			Expect(session([]string{"--read-only"}, pickJob("--read-only=false"))).To(Equal(exitcode.OK))

			Expect(results).To(HaveLen(1))
			Expect(results[0].ExitCode).To(Equal(exitcode.Usage), "stderr: %s", results[0].Stderr)
			Expect(string(results[0].Stderr)).To(ContainSubstring("cannot loosen"))
			Expect(t.recorded()).To(BeEmpty())
		})

		It("still lets a read through", func() {
			Expect(session([]string{"--read-only"}, tui.Invocation{Args: []string{"facility", "list"}})).To(Equal(exitcode.OK))

			Expect(results[0].ExitCode).To(Equal(exitcode.OK), "stderr: %s", results[0].Stderr)
			Expect(t.recorded()).To(HaveLen(1))
		})
	})

	When("the session was started without it", func() {
		It("sends the write", func() {
			Expect(session(nil, pickJob())).To(Equal(exitcode.OK))

			Expect(results[0].ExitCode).To(Equal(exitcode.OK), "stderr: %s", results[0].Stderr)
			Expect(t.recorded()).To(HaveLen(1))
		})

		It("leaves no read-only session behind for the next command in the shell", func() {
			Expect(session([]string{"--read-only"})).To(Equal(exitcode.OK))

			Expect(c.run("picking", "add-pick-job", "--data", `{"pickLineItems":[]}`)).To(Equal(exitcode.OK), c.errOut())
			Expect(t.recorded()).To(HaveLen(1))
		})
	})

	When("the session was started with --timeout", func() {
		It("bounds every run that does not give its own", func() {
			c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
				<-r.Context().Done()
			})

			Expect(session([]string{"--timeout", "50ms"},
				tui.Invocation{Args: []string{"picking", "get-pick-job", "--pick-job-id", "pj-1"}},
			)).To(Equal(exitcode.OK))

			Expect(results[0].ExitCode).NotTo(Equal(exitcode.OK))
			Expect(string(results[0].Stderr)).To(ContainSubstring("deadline exceeded"))
		})
	})
})
