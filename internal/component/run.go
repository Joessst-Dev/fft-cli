package component

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// killGrace is how long a component gets to exit after its context is cancelled
// before it is killed.
//
// It exists because the emulator is the shape of component this has to be right
// for: Ctrl-C should let it drain its in-flight requests and shut down cleanly, and
// a component that ignores the signal should still not leave fft hanging at a shell
// prompt that never comes back.
const killGrace = 10 * time.Second

// Streams are the three the component inherits.
//
// It inherits them rather than having them captured and relayed, which is what
// makes the output contract survive the process boundary: `fft something -o json |
// jq` puts the component's stdout in the pipe directly, with no buffering and
// nothing of fft's mixed into it.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Run executes a component command and waits for it.
//
// args are the arguments after the command name, passed through untouched — the
// cobra stub disables flag parsing, so a component owns its own flag syntax
// completely and fft never has to be taught it.
func Run(ctx context.Context, c Component, args []string, env []string, streams Streams) error {
	path := c.ExecPath()
	if path == "" || !c.Installed {
		return &NotInstalledError{Name: c.Name, Command: "fft " + c.Name}
	}

	// exec.CommandContext, never syscall.Exec: replacing the process would be a
	// little faster on Unix and does not exist on Windows, and one dispatch mechanism
	// that behaves identically everywhere is worth more than that.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Stdin = streams.In
	cmd.Stdout = streams.Out
	cmd.Stderr = streams.Err

	// The working directory is deliberately left alone. `fft emulator --seed ./fixtures`
	// means the fixtures next to the user, not the ones next to the binary; pointing
	// the child at its own directory would quietly re-root every relative path the
	// user typed.

	// Cancel politely, then insist. Go's default is to SIGKILL on cancellation, which
	// for a server means dropping whatever it was serving.
	cmd.Cancel = func() error { return cmd.Process.Signal(interruptSignal()) }
	cmd.WaitDelay = killGrace

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run the %s component: %w", c.Name, err)
	}

	err := cmd.Wait()
	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &ExitError{Name: c.Name, Code: exitErr.ExitCode()}
	}
	return fmt.Errorf("run the %s component: %w", c.Name, err)
}

// interruptSignal is what a cancelled component is asked to stop with.
//
// Windows has no way for one process to send another a SIGINT, so there it is the
// abrupt one — in practice a component sharing the console has already received the
// Ctrl-C event by the time this fires, and this is the backstop for the case where
// the cancellation came from somewhere else.
func interruptSignal() os.Signal {
	if runtime.GOOS == "windows" {
		return os.Kill
	}
	return os.Interrupt
}

// ExitError is a component that exited non-zero.
//
// It is silent, and that is the whole reason it is a type. The component wrote its
// own diagnostics to the stderr it inherited — the user has already read them — so
// fft printing "Error: exit status 6" underneath would be a second, worse message
// about something already explained. What fft still owes the caller is the exit
// code, which is the part a script reads.
//
// A component is documented to use fft's own table (internal/exitcode), so a 6 out
// of a component means the same thing as a 6 out of fft.
type ExitError struct {
	// Name is the component's name, for a caller that wants to say which one failed.
	Name string

	// Code is the status the component exited with.
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("the %s component exited %d", e.Name, e.Code)
}

func (e *ExitError) ExitCode() int {
	if e.Code <= 0 {
		// Killed by a signal, or an exit status the platform could not report. It
		// failed, and fft has nothing more specific to say about how.
		return exitcode.General
	}
	return e.Code
}

// Silent reports that this error has already been explained to the user by the
// process that caused it. See [ExitError].
func (e *ExitError) Silent() bool { return true }
