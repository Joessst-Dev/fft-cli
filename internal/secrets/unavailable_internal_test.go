package secrets

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"syscall"

	dbus "github.com/godbus/dbus/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/zalando/go-keyring"
)

// dbusErr builds the error godbus delivers for a failed call: a value with the
// wire protocol's name, and a body that does not contain it.
func dbusErr(name string) error {
	return dbus.Error{Name: name, Body: []any{"the bus said so"}}
}

var _ = Describe("classifying an unreachable keychain", func() {
	DescribeTable("there is no keychain here",
		func(err error, goos string) {
			Expect(unavailableOn(err, goos)).To(BeTrue())
		},
		Entry("no backend for this platform", keyring.ErrUnsupportedPlatform, "linux"),
		Entry("nothing owns the secrets name", dbusErr(dbusServiceUnknown), "linux"),
		Entry("the name has no owner", dbusErr(dbusNameHasNoOwner), "linux"),
		Entry("there is no bus server", dbusErr(dbusNoServer), "linux"),
		// The whole point of matching the struct rather than the text: an error
		// arrives through several layers of fmt.Errorf before it is classified.
		Entry("wrapped several times",
			fmt.Errorf("write %q: %w", "fft:prd:password", fmt.Errorf("open session: %w", dbusErr(dbusServiceUnknown))), "linux"),
		// The WSL case: godbus autolaunches a bus by running dbus-launch, and a stock
		// distribution has neither.
		Entry("dbus-launch is not installed",
			&exec.Error{Name: "dbus-launch", Err: exec.ErrNotFound}, "linux"),
		Entry("dbus-launch failed", &exec.ExitError{}, "linux"),
		Entry("the bus socket is not there",
			&net.OpError{Op: "dial", Net: "unix", Err: syscall.ENOENT}, "linux"),
		Entry("nothing is listening on the bus socket",
			&net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED}, "linux"),
		Entry("godbus could not find an address",
			errors.New("dbus: couldn't determine address of session bus"), "linux"),
	)

	DescribeTable("the keychain is there and said no",
		func(err error, goos string) {
			Expect(unavailableOn(err, goos)).To(BeFalse())
		},
		Entry("nothing went wrong", nil, "linux"),
		Entry("the secret is simply absent", keyring.ErrNotFound, "linux"),
		Entry("the secret is too big for the backend", keyring.ErrSetDataTooBig, "windows"),
		Entry("permission was refused", dbusErr("org.freedesktop.DBus.Error.AccessDenied"), "linux"),
		// A prompter that hung is a prompter that exists.
		Entry("the call timed out", dbusErr("org.freedesktop.DBus.Error.NoReply"), "linux"),
		// A locked keychain is a keychain. Falling back to a file here would write
		// cleartext to sidestep a password the user has and could type.
		Entry("the collection is locked", dbusErr("org.freedesktop.Secret.Error.IsLocked"), "linux"),
		Entry("no such object", dbusErr("org.freedesktop.Secret.Error.NoSuchObject"), "linux"),
		Entry("an unrecognised failure", errors.New("something else went wrong"), "linux"),
		// A read or a write error means the connection was established, so the bus
		// was there and something answered on it — a dbus-daemon restarted by a
		// package upgrade mid-command, say. Offering a cleartext file to a user
		// whose keychain is working is the one mistake this file exists to avoid.
		Entry("the established connection dropped mid-call",
			&net.OpError{Op: "read", Net: "unix", Err: syscall.ECONNRESET}, "linux"),
		Entry("the established connection was written to after closing",
			&net.OpError{Op: "write", Net: "unix", Err: syscall.EPIPE}, "linux"),
		// The socket is there; we are not allowed to open it. That is a keychain
		// somebody else owns, not a missing one.
		Entry("the bus socket refused us", &net.OpError{Op: "dial", Net: "unix", Err: syscall.EACCES}, "linux"),
		// The single most important entry here. go-keyring reaches the macOS Keychain
		// by running /usr/bin/security, so a non-zero exit is the user clicking Deny —
		// the opposite of what the same shape means on Linux.
		Entry("the macOS Keychain refused us", &exec.ExitError{}, "darwin"),
		Entry("/usr/bin/security is missing", &exec.Error{Name: "security", Err: exec.ErrNotFound}, "darwin"),
	)
})

var _ = Describe("the keychain store, with no keychain behind it", func() {
	// gone is a backend that is not there. The D-Bus shape, not the exec one, so
	// that this reads the same on all three platforms CI runs: the exec shape is
	// deliberately classified only off darwin.
	gone := func() error { return dbusErr(dbusServiceUnknown) }

	missing := keyringStore{
		get:    func(string, string) (string, error) { return "", gone() },
		set:    func(string, string, string) error { return gone() },
		remove: func(string, string) error { return gone() },
	}

	DescribeTable("reports the sentinel, whichever way it was asked",
		func(call func() error) {
			err := call()

			Expect(err).To(MatchError(ErrKeyringUnavailable))
			// The backend's own account survives, so a developer can still see what
			// the keychain actually said.
			Expect(err.Error()).To(ContainSubstring("the bus said so"))
			// And it is not mistaken for a secret that merely is not there.
			Expect(err).NotTo(MatchError(ErrNotFound))
		},
		Entry("reading", func() error { _, err := missing.Get("fft:prd:password"); return err }),
		Entry("writing", func() error { return missing.Set("fft:prd:password", "s3cret") }),
		Entry("deleting", func() error { return missing.Delete("fft:prd:password") }),
	)

	It("still reports an absent secret as absent", func() {
		store := keyringStore{get: func(string, string) (string, error) { return "", keyring.ErrNotFound }}

		_, err := store.Get("fft:prd:password")

		Expect(err).To(MatchError(ErrNotFound))
		Expect(err).NotTo(MatchError(ErrKeyringUnavailable))
	})

	It("leaves an unclassified failure wrapped in the operation that hit it", func() {
		store := keyringStore{set: func(string, string, string) error { return errors.New("boom") }}

		err := store.Set("fft:prd:password", "s3cret")

		Expect(err).To(MatchError(ContainSubstring(`write "fft:prd:password" to the keychain`)))
		Expect(err).NotTo(MatchError(ErrKeyringUnavailable))
	})
})
