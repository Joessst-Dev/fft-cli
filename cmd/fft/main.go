// Command fft is a command-line client for the fulfillmenttools API.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// hinted is implemented by errors that know which command would fix them. A hint
// is the difference between an error a user can act on and one they have to go
// and read the manual about.
type hinted interface{ Hint() string }

// silent is implemented by errors that have already been reported to the user by
// whatever produced them.
//
// There is exactly one: a component that exited non-zero. It wrote its own
// diagnostics to the stderr it inherited from fft, so the user has read them
// already, and "Error: the emulator component exited 6" underneath would be fft
// narrating a failure it did not witness. The exit code still has to come through,
// which is why the error exists at all rather than being swallowed.
type silent interface{ Silent() bool }

func main() {
	// SIGINT/SIGTERM cancel the root context, so in-flight requests unwind through
	// the normal error path instead of being killed mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The zero Deps is completed with the real config store, keychain and printer
	// once the global flags have been parsed. Specs pass a Deps of fakes instead.
	err := newRootCmd(&Deps{}).ExecuteContext(ctx)

	// Diagnostics go to stderr — always. stdout carries data only, so that
	// `fft ... -o json | jq` is never contaminated by an error message.
	//
	// This is the only os.Exit in the program. Commands return errors; main
	// decides what they mean.
	os.Exit(report(os.Stderr, err))
}

// report writes err the way a user should see it and returns the exit code it
// means. The spec harness calls this too, so a spec can never assert on a message
// that no terminal would ever have shown.
func report(w io.Writer, err error) int {
	err = explainKeyring(err)
	writeError(w, err)
	return exitcode.FromError(err)
}

// writeError reports err the way a user should see it: the message, then the one
// thing they could do about it. A cancelled context is the user pressing Ctrl-C,
// which needs no explaining.
func writeError(w io.Writer, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}

	var quiet silent
	if errors.As(err, &quiet) && quiet.Silent() {
		return
	}

	fmt.Fprintln(w, "Error:", err)

	var usageErr exitcode.UsageError
	var h hinted

	switch {
	case errors.As(err, &usageErr):
		fmt.Fprintln(w, "\nRun 'fft --help' for usage.")
	case errors.As(err, &h) && h.Hint() != "":
		fmt.Fprintln(w, "\n"+h.Hint())
	}
}
