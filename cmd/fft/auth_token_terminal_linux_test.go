//go:build linux

package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// openTerminal returns the slave end of a fresh pseudo-terminal: a real file
// descriptor that isatty answers yes for, which no buffer can be. It is the only
// way to put the terminal refusal under test, because the check asks the stream
// the token would be written to.
func openTerminal() *os.File {
	GinkgoHelper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		Skip("no pseudo-terminal available: " + err.Error())
	}
	DeferCleanup(master.Close)

	Expect(unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0)).To(Succeed())
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	Expect(err).NotTo(HaveOccurred())

	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(slave.Close)

	return slave
}

// countingRenewer records the mints, so a spec can prove that a refusal did not
// spend one.
type countingRenewer struct{ renewals int }

func (r *countingRenewer) Token(context.Context) (string, error) { return "cached-id-token", nil }

func (r *countingRenewer) Renew(context.Context) (auth.Token, error) {
	r.renewals++
	return auth.Token{ID: "fresh-id-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

var _ = Describe("fft auth token on a terminal", func() {
	var (
		c       *cli
		renewer *countingRenewer
		tty     *os.File
	)

	BeforeEach(func() {
		c = newCLI()
		c.fakeAPI(func(http.ResponseWriter, *http.Request) {})
		renewer = &countingRenewer{}
		c.deps.NewTokenSource = func(config.Project, secrets.Store, func() time.Time, io.Writer) (auth.TokenSource, error) {
			return renewer, nil
		}
		tty = openTerminal()
	})

	runOnTerminal := func(args ...string) int {
		c.stderr.Reset()
		return execute(context.Background(), c.deps, args, c.stdin, tty, &c.stderr)
	}

	It("refuses without --raw and mints nothing: a refused run must not spend a sign-in", func() {
		Expect(runOnTerminal("auth", "token", "--refresh")).To(Equal(exitcode.Usage))

		Expect(renewer.renewals).To(BeZero())
		Expect(c.errOut()).To(ContainSubstring("scrollback"))
	})

	It("mints and prints once --raw says it was meant", func() {
		Expect(runOnTerminal("auth", "token", "--refresh", "--raw")).To(Equal(exitcode.OK))

		Expect(renewer.renewals).To(Equal(1))
	})
})
