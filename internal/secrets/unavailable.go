package secrets

import (
	"errors"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	dbus "github.com/godbus/dbus/v5"
	"github.com/zalando/go-keyring"
)

// ErrKeyringUnavailable means this machine has no OS keychain fft can reach at
// all: nothing owning the Secret Service on the session bus, no session bus to
// look on, or a platform go-keyring has no backend for. A WSL distribution is
// the common case — it is Linux, so go-keyring compiles in the D-Bus provider,
// and there is nothing on the other end of it.
//
// It is deliberately narrow. A keychain that is there and said no — locked, a
// prompt the user denied, a secret the backend thinks is too big — is not this,
// because the answer to those is to unlock or to retry, and not to start writing
// the password to a file.
var ErrKeyringUnavailable = errors.New("no OS keychain is available")

// The D-Bus error names that mean nothing is listening for us.
//
// They are matched instead of the message text because they are the wire
// protocol's own identifiers and do not change, while the text does — and
// because [dbus.Error.Error] returns the human-readable body, so the name is
// only reachable through the struct.
const (
	dbusServiceUnknown = "org.freedesktop.DBus.Error.ServiceUnknown"
	dbusNameHasNoOwner = "org.freedesktop.DBus.Error.NameHasNoOwner"
	dbusNoServer       = "org.freedesktop.DBus.Error.NoServer"
)

// The one failure godbus reports as a bare errors.New, with no type and no
// sentinel to match on (conn.go and conn_other.go): it could not work out where
// the session bus is. Matching its text is the only option, so it is done last
// and only for this one, rather than as a general sweep of the message.
const noSessionBusText = "couldn't determine address of session bus"

// unavailableError carries the sentinel together with the backend's own account
// of the failure.
type unavailableError struct{ cause error }

func (e *unavailableError) Error() string {
	return "no OS keychain is available on this machine: " + e.cause.Error()
}

// Unwrap returns both the sentinel and the cause, so that errors.Is finds
// [ErrKeyringUnavailable] while the dbus text a developer needs is still in the
// message.
func (e *unavailableError) Unwrap() []error { return []error{ErrKeyringUnavailable, e.cause} }

// unavailable reports whether err means there is no keychain to talk to.
func unavailable(err error) bool { return unavailableOn(err, runtime.GOOS) }

// unavailableOn is [unavailable] with the platform passed in, because the answer
// depends on it and a spec cannot change runtime.GOOS.
func unavailableOn(err error, goos string) bool {
	if err == nil {
		return false
	}

	// No backend was compiled in for this platform at all.
	if errors.Is(err, keyring.ErrUnsupportedPlatform) {
		return true
	}

	// A bus answered. Whether that answer means "there is no keychain here" or
	// "the keychain you have says no" is the whole question, and the name is the
	// only thing that distinguishes them — so an unrecognised name is deliberately
	// *not* unavailable, and must not fall through to the text match below.
	// Everything under org.freedesktop.Secret.Error.* (IsLocked, NoSuchObject) is
	// among those: a locked keychain is a keychain. So is AccessDenied, and so is
	// NoReply — a prompter that hung is one that exists.
	//
	// Both shapes, because the property that only one of them reaches here is
	// go-keyring's to change, not ours: godbus delivers a remote error as a
	// dbus.Error value (finalizeWithError) and reserves *dbus.Error for the server
	// side, so today only the value form arrives. Checking both costs a line and
	// stops that staying true by accident.
	var dbusErr dbus.Error
	var dbusErrPtr *dbus.Error
	switch {
	case errors.As(err, &dbusErr):
		return knownAbsentName(dbusErr.Name)
	case errors.As(err, &dbusErrPtr):
		return knownAbsentName(dbusErrPtr.Name)
	}

	// On Linux the only binary this path runs is dbus-launch — godbus autolaunches
	// a session bus when it cannot find one, and a stock WSL distribution has
	// neither the bus nor the launcher — and the only thing it dials is the bus
	// socket. So a failure to exec or to connect means there is nothing here.
	//
	// On macOS the same two shapes mean the opposite: go-keyring reaches the
	// Keychain by running /usr/bin/security, where a non-zero exit is the Keychain
	// refusing us, which is a keychain that very much exists. Hence the guard.
	// Windows goes through wincred and produces neither shape.
	if goos != "darwin" {
		var execErr *exec.Error
		var exitErr *exec.ExitError
		if errors.As(err, &execErr) || errors.As(err, &exitErr) {
			return true
		}

		// A dial that found nothing to dial, and only that. A read or a write error
		// comes from a connection that was established, so the bus was there and
		// something was on it; a dial refused with EACCES found a socket it was not
		// allowed to open. Both are a keychain that exists — and mistaking a
		// dbus-daemon restarted mid-session for "no keychain here" would offer a
		// cleartext file to a user who has a working one.
		var netErr *net.OpError
		if errors.As(err, &netErr) && netErr.Op == "dial" &&
			(errors.Is(netErr, syscall.ENOENT) || errors.Is(netErr, syscall.ECONNREFUSED)) {
			return true
		}

		// Guarded like the rest: this is an unanchored substring of a message, the
		// weakest match in the file, and a session bus is a thing only the D-Bus
		// backend has. There is no reading of it that should decide anything on a
		// Mac, where go-keyring never opens one.
		if strings.Contains(err.Error(), noSessionBusText) {
			return true
		}
	}

	return false
}

// knownAbsentName reports whether a D-Bus error name means nothing is there to
// answer, as opposed to something that answered and refused.
func knownAbsentName(name string) bool {
	switch name {
	case dbusServiceUnknown, dbusNameHasNoOwner, dbusNoServer:
		return true
	default:
		return false
	}
}
