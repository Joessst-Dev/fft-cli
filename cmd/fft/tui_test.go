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
