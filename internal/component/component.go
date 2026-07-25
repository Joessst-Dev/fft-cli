// Package component is fft's extension point: a way for anyone to add a building
// block to the CLI without a pull request into this repository.
//
// # What a component is
//
// A directory holding a manifest and an executable:
//
//	<root>/emulator/
//	  component.yaml
//	  bin/fft-emulator
//
// fft reads the manifest at startup, registers a cobra command for everything it
// declares, and — when one of those commands is run — executes the binary with the
// remaining arguments and an environment it builds itself. The child inherits
// stdin, stdout and stderr, so the output contract is passed straight through, and
// its exit code becomes fft's.
//
// # The environment is the interface
//
// A component is a *headless fft consumer*. It never opens the keychain and never
// reads the config file; it receives the FFT_* variables [config.FromEnv] already
// defines, which is the same contract a CI job uses. That is why this package can
// be small: the handover was designed before the extension point was.
//
// What it hands over is decided by the session level the manifest declares, and
// [Environ] is the only place that decision is made — see its documentation for
// why a component never receives the Firebase API key.
//
// # No PATH scanning
//
// Unlike kubectl, an executable called fft-something on $PATH is *not* a command.
// The tree comes from the compiled-in first-party table and from the managed root,
// and from nowhere else, so what `fft --help` prints never depends on what else is
// installed on the machine. Same instinct as the read-only gate: an invariant that
// says the surprising thing cannot happen is worth more than a warning that it did.
//
// # Trust
//
// A component is code, and it runs as the user who ran fft. The manifest *declares*
// intent — whether a command mutates the tenant, which session it needs — and fft
// enforces what it can: it refuses a mutating component against a read-only
// project, and it never exports a credential the declared session did not ask for.
// It cannot stop a component that lies. `fft component install` says so, in those
// words, before it writes anything.
package component

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// APIVersion is the manifest contract this build speaks.
//
// A manifest declaring anything else is refused rather than read leniently: the
// fields fft would silently ignore are exactly the ones a future version added to
// change what a component *does*, and quietly running it with half its manifest
// honoured is worse than not running it at all.
const APIVersion = 1

// Kind is what a component extends.
type Kind string

const (
	// KindCommand adds subcommands to fft.
	KindCommand Kind = "command"

	// KindTransport delivers emulator events to a broker fft has never heard of.
	// It is addressed by the emulator over a line protocol rather than by the user,
	// so it declares target types instead of commands.
	KindTransport Kind = "transport"
)

// Session is how much of the caller's tenant session a component command receives.
type Session string

const (
	// SessionNone hands over no credential at all. It is the right answer more often
	// than it looks: the emulator serves a fake tenant and has no use for a token.
	SessionNone Session = "none"

	// SessionRead hands over a short-lived id token and forces FFT_READ_ONLY, so the
	// component's own fft calls are gated the way the parent's would have been.
	SessionRead Session = "read"

	// SessionWrite hands over the same session without the forced read-only.
	SessionWrite Session = "write"
)

// Component is one installed component: its manifest, and where it lives.
type Component struct {
	Manifest

	// Dir is the component's own directory, absolute.
	Dir string

	// FirstParty marks a component fft ships and knows about at compile time. Its
	// command tree comes from [FirstParty], not from the manifest on disk — see that
	// function for why.
	FirstParty bool

	// Installed reports whether the executable is actually there. A first-party
	// component is registered whether or not it is, so that `fft emulator` can
	// explain how to get it rather than not existing.
	Installed bool
}

// ExecPath is the absolute path of the component's executable, with the platform's
// suffix if that is how it is spelled on disk.
//
// Not called Exec, though that is what the manifest field is called: a method of
// that name on the embedding struct would shadow the embedded field, and the field
// is the one an installer has to be able to write.
func (c Component) ExecPath() string {
	if c.Dir == "" || c.Exec == "" {
		return ""
	}

	path := filepath.Join(c.Dir, filepath.FromSlash(c.Exec))
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if suffixed := execName(path); suffixed != path {
		if _, err := os.Stat(suffixed); err == nil {
			return suffixed
		}
	}
	return path
}

// Command finds a declared command by name.
func (c Component) Command(name string) (Command, bool) {
	for _, cmd := range c.Commands {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return Command{}, false
}

// Delivers reports whether a transport component handles this subscription target
// type.
func (c Component) Delivers(target string) bool {
	return slices.Contains(c.Targets, target)
}

// NotInstalledError is a component whose manifest fft has but whose executable it
// has not — either because it ships first-party and was never installed, or because
// an install was interrupted.
//
// It exits [exitcode.Config] rather than 1: like a missing project, it is a
// configuration problem with a command that fixes it, and [NotInstalledError.Hint]
// is that command.
type NotInstalledError struct {
	// Name is the component's name.
	Name string

	// Command is the command path the user typed, for the message.
	Command string
}

func (e *NotInstalledError) Error() string {
	return fmt.Sprintf("%s is not installed", e.Command)
}

func (e *NotInstalledError) ExitCode() int { return exitcode.Config }

func (e *NotInstalledError) Hint() string {
	return fmt.Sprintf("Run 'fft component install %s' to install it.", e.Name)
}

// execName gives an executable the suffix the platform expects, so a manifest can
// name one binary and be right on all three.
func execName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
