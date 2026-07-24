package component

import (
	"path/filepath"

	"github.com/Joessst-Dev/fft-cli/internal/config"
)

// The components fft ships itself.
//
// # Why they are compiled in rather than merely installable
//
// A first-party component's command is registered whether or not its binary is on
// the disk. That one decision is what keeps four things true:
//
//   - `fft --help` lists the same commands on every machine, so what fft *is* does
//     not depend on what has been installed.
//   - `fft gen-docs` renders the same tree every time, so docs/reference/ cannot
//     drift with a developer's local installs.
//   - skill_drift_test.go still resolves every `fft …` invocation in the skill,
//     including the ones in references/emulator.md.
//   - Typing the command when it is missing produces an explanation and the one
//     command that fixes it, rather than "unknown command".
//
// So for a first-party component this table is authoritative for the *command
// tree* — names, help, session, mutates, claims — and the manifest on disk supplies
// only the physical facts: which version is installed, and which binary to run. An
// old installed component therefore cannot change what fft's help says, and cannot
// widen its own session by shipping a manifest that asks for more.
//
// A third-party component has no such table, so its manifest is authoritative for
// everything. That is the trade for not being shipped by us, and it is why those
// commands are listed under their own heading in `fft --help`.
var firstParty = []Manifest{{
	APIVersion:  APIVersion,
	Name:        "emulator",
	Kind:        KindCommand,
	Source:      DefaultRepo,
	Exec:        "bin/fft-emulator",
	Description: "Run a local offline fulfillmenttools API emulator",

	// `fft emulator emit` talks to a running emulator, at the address the emulator's
	// own startup recipe told the user to export. It is the one FFT_ variable a
	// component may ask for — see [forwardable] — and it is asked for here so that
	// `fft emulator emit` keeps working exactly as it always has.
	Env: []string{config.EnvBaseURL},

	Commands: []Command{{
		Name:  "emulator",
		Short: "Run a local offline fulfillmenttools API emulator",

		// session: none, and it is the case that proves the level is worth having. The
		// emulator serves a fake tenant; it has no use for a credential, and until this
		// split it ran in-process inside a binary holding one.
		Session: SessionNone,
		Long: `Run a local server that mimics the fulfillmenttools API.

Every operation the API has is addressable on the emulator. The top-level
collections (facilities, listings, stocks, orders, …) are stateful: a POST is
remembered, a GET reflects it, versions and pagination work. Everything else is
answered from a response synthesized from the spec — reachable, but not remembered.

The emulator makes no request to any tenant and holds all state in memory, so it
dies with the process. Point fft at it with the FFT_* recipe it prints on startup;
'fft project add' does not work against it, because signing in reaches Google's
identity service, which a local server cannot stand in for.`,
	}},
}}

// Option configures a [Registry].
type Option func(*Registry)

// WithFirstParty replaces the compiled-in first-party table.
//
// It exists for the specs, which need a first-party component to assert on without
// waiting for one to be shipped, and for `fft gen-docs`, which needs the table
// without the disk.
func WithFirstParty(table []Manifest) Option {
	return func(r *Registry) { r.firstParty = table }
}

// firstPartyComponents resolves a table into components rooted at root.
//
// A first-party component is always returned, with Installed reporting whether its
// binary is actually there.
func firstPartyComponents(table []Manifest, root string) []Component {
	if len(table) == 0 {
		return nil
	}

	out := make([]Component, 0, len(table))
	for _, m := range table {
		dir := ""
		if root != "" {
			dir = filepath.Join(root, m.Name)
		}
		out = append(out, newComponent(m, dir, true))
	}
	return out
}
