// Command fft-emulator runs a local server that mimics the fulfillmenttools API.
//
// It is the emulator component of fft, and the reference implementation of the
// component contract — which is not an accident. The emulator is the thing that
// proves a component can be substantial: it binds a port, holds state, and starts
// sub-components of its own, and it does all of that with no tenant credential at
// all, because it declares session: none.
//
// It is normally reached as `fft emulator`, which finds this binary and runs it with
// the arguments untouched. Run directly it behaves identically; nothing here depends
// on having been started by fft.
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

// hinted is implemented by errors that know which command would fix them, as in fft
// itself — a component is documented to use fft's exit codes, and an error worth an
// exit code is usually worth a next step too.
type hinted interface{ Hint() string }

func main() {
	// SIGINT/SIGTERM cancel the root context, so Ctrl-C drains the server rather than
	// dropping whatever it was serving. When fft started this process it cancels the
	// same way, so the two paths behave alike.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := newRootCmd().ExecuteContext(ctx)
	writeError(os.Stderr, err)

	os.Exit(exitcode.FromError(err))
}

// writeError reports err the way a user should see it: the message, then the one
// thing they could do about it. On stderr — stdout is data here as everywhere.
func writeError(w io.Writer, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}

	fmt.Fprintln(w, "Error:", err)

	var usageErr exitcode.UsageError
	var h hinted

	switch {
	case errors.As(err, &usageErr):
		fmt.Fprintln(w, "\nRun 'fft emulator --help' for usage.")
	case errors.As(err, &h) && h.Hint() != "":
		fmt.Fprintln(w, "\n"+h.Hint())
	}
}
