package component

import "path/filepath"

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
//
// # It is empty, for now
//
// The emulator moves in here when it moves out of the binary. Until then the
// machinery is exercised by third-party components and by the specs, which pass
// their own table to [WithFirstParty] — there is deliberately no half-state where a
// command is both built in and a first-party stub.
var firstParty []Manifest

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
