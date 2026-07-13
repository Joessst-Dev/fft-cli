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
	writeError(os.Stderr, err)

	// This is the only os.Exit in the program. Commands return errors; main
	// decides what they mean.
	os.Exit(exitcode.FromError(err))
}

// writeError reports err the way a user should see it: the message, then the one
// thing they could do about it. A cancelled context is the user pressing Ctrl-C,
// which needs no explaining.
func writeError(w io.Writer, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
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
