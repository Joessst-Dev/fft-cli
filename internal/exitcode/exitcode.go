// Package exitcode maps errors to fft's process exit codes.
//
// The codes are documented for scripts and agents in the skill's troubleshooting
// reference (internal/skill/assets/references/troubleshooting.md), which the docs
// site publishes as the Troubleshooting guide.
package exitcode

import (
	"context"
	"errors"
)

// Exit codes. These are part of the CLI's contract with scripts — changing one
// is a breaking change.
const (
	OK          = 0 // success
	General     = 1 // unclassified failure
	Usage       = 2 // bad flags or arguments
	Config      = 3 // no active project, or the config is unusable
	Auth        = 4 // authentication failed (401)
	Forbidden   = 5 // authenticated but not permitted (403)
	NotFound    = 6 // the resource does not exist (404)
	Conflict    = 7 // optimistic-locking version conflict (409)
	Partial     = 8 // a bulk operation succeeded for some items and failed for others
	Unavailable = 9 // upstream is unreachable or erroring (5xx, timeout)

	// ReadOnly is a write refused against a read-only project. It is not a
	// Forbidden: a 403 means the tenant said no, and this means fft did. Nothing
	// left the machine, so a script that retries a 5 with other credentials must
	// not retry this one.
	ReadOnly = 10

	// ProblemsFound is a check that ran to the end and found what it looks for:
	// `fft network check` reporting gaps. The command worked; its answer is "no".
	// A CI job gates on it, and must be able to tell it from a check that could not
	// run at all, which exits with that failure's own code.
	ProblemsFound = 11

	Interrupted = 130
)

// coded is implemented by errors that know their own exit code. Packages
// declare their sentinels to satisfy it rather than importing this one.
type coded interface{ ExitCode() int }

// UsageError marks an error as a usage problem so cobra's argument and flag
// validation surfaces as exit code 2 rather than a generic failure.
type UsageError struct{ Err error }

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }
func (e UsageError) ExitCode() int { return Usage }

// FromError classifies an error into an exit code. A cancellation is an interrupt
// whatever wraps it; errors that implement ExitCode() decide for themselves; an
// expired deadline is the upstream not answering in time; everything else is a
// general failure.
//
// The deadline comes after the explicit code so that an error that names its own
// (a sign-in that timed out is still a 4) is never overruled by the cause inside it.
func FromError(err error) int {
	switch {
	case err == nil:
		return OK
	case errors.Is(err, context.Canceled):
		return Interrupted
	}

	var c coded
	if errors.As(err, &c) {
		return c.ExitCode()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Unavailable
	}
	return General
}

// meanings are the short descriptions the troubleshooting reference's table gives
// each code. Every constant above must have one; a spec reads the const block to
// hold that.
var meanings = map[int]string{
	OK:            "success",
	General:       "unclassified failure",
	Usage:         "bad flags or arguments",
	Config:        "no active project, or the config is unusable",
	Auth:          "authentication failed",
	Forbidden:     "authenticated, but not permitted",
	NotFound:      "not found",
	Conflict:      "version conflict",
	Partial:       "partial bulk write",
	Unavailable:   "upstream unreachable or erroring",
	ReadOnly:      "read-only: fft refused a write, nothing was sent",
	ProblemsFound: "a check ran and found problems",
	Interrupted:   "interrupted",
}

// Meaning describes code in a few words, for a person reading a result rather than
// a script branching on it. A code fft never exits with is described as unknown.
func Meaning(code int) string {
	if m, ok := meanings[code]; ok {
		return m
	}
	return "unknown exit code"
}
