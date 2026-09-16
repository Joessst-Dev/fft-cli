package main

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

// execute runs one fft command line against deps and returns its exit code.
//
// It is the only way a command line is run: main runs the process's through it,
// the spec harness runs every spec's, and the TUI runs each of its requests. So a
// guarantee made here — the way an error is reported, what is recorded about the
// run — holds for all three, and every spec in the suite exercises the path the
// shell and the UI take.
func execute(ctx context.Context, deps *Deps, args []string, in io.Reader, out, errw io.Writer) int {
	return executeRoot(ctx, newRootCmd(deps), args, in, out, errw)
}

// executeRoot is [execute] on a tree the caller has already built against its
// deps — the TUI's runner builds it first to find out what the command line
// resolves to, and building the whole tree a second time would buy nothing.
//
// A nil stream leaves cobra's default in place, and for out that is not the same
// thing as os.Stdout. Cobra's Print family — an unknown help topic, for one —
// writes to the output stream only when one was set, and to stderr otherwise; so
// setting the process's own stdout would move those messages onto the stream a
// script is piping into jq. The process therefore names no streams at all.
func executeRoot(ctx context.Context, root *cobra.Command, args []string, in io.Reader, out, errw io.Writer) int {
	root.SetArgs(args)
	if in != nil {
		root.SetIn(in)
	}
	if out != nil {
		root.SetOut(out)
	}
	if errw != nil {
		root.SetErr(errw)
	}

	// ExecuteContextC rather than ExecuteContext: it returns the command that ran
	// even when it failed, which is what request history needs to name the
	// operation a run addressed.
	_, err := root.ExecuteContextC(ctx)

	// Diagnostics go to stderr — always. stdout carries data only, so that
	// `fft ... -o json | jq` is never contaminated by an error message.
	return report(root.ErrOrStderr(), err)
}
