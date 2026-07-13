// Package exitcode maps errors to the process exit codes documented in `fft help exit-codes`.
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

// FromError classifies an error into an exit code. Errors that implement
// ExitCode() decide for themselves; everything else is a general failure.
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
	return General
}
