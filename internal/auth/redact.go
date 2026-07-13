package auth

import (
	"github.com/Joessst-Dev/fft-cli/internal/httplog"
)

// redacted is what a secret is replaced with.
const redacted = httplog.Redacted

// redactString removes the API key from s, along with any of the extra secrets
// given (a token, a password) that appear in it verbatim.
//
// The rules live in httplog, which is also where --debug's dump is redacted:
// there is one set of them, and adding a secret to it protects both paths at once.
func redactString(s string, extra ...string) string {
	return httplog.Redact(s, extra...)
}

// redactedError hides secrets in an error's message while leaving the chain
// intact, so errors.Is and errors.As keep working.
//
// The wrapped error is still reachable through Unwrap — this guards the printing
// path, which is the one a user and a pasted bug report actually see.
type redactedError struct {
	err   error
	extra []string
}

// redact wraps err so that printing it cannot leak the API key or any of extra.
// It returns nil for a nil error, so it can wrap a call's result directly.
func redact(err error, extra ...string) error {
	if err == nil {
		return nil
	}
	return &redactedError{err: err, extra: extra}
}

func (e *redactedError) Error() string { return redactString(e.err.Error(), e.extra...) }
func (e *redactedError) Unwrap() error { return e.err }
