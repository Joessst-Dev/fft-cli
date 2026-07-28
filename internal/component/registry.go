package component

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// EnvRoot overrides where components are installed and looked for.
//
// Set-but-empty is not the same as unset: it means *no components*, and it is how
// `fft gen-docs` and the specs pin a tree that does not depend on what the machine
// happens to have installed. Generated documentation that changes because the
// developer installed something is documentation that fails CI for a reason nobody
// can see in the diff.
const EnvRoot = "FFT_COMPONENT_DIR"

// ManifestName is the file that makes a directory a component.
const ManifestName = "component.yaml"

// dirMode is what the component root and a component's own directory are created
// as. Executables, not secrets: 0700 would be a claim that is not true, and would
// stop a component being run by anything but the user who installed it — which is
// wrong on a shared build box.
const dirMode = 0o755

// Root is where components live: $FFT_COMPONENT_DIR, else $XDG_DATA_HOME/fft/components,
// else ~/.local/share/fft/components.
//
// Data, not config and not cache. A component is neither something the user edits
// nor something fft can regenerate by asking again, which is what the other two
// directories mean.
//
// The second return value reports whether components are enabled at all; it is
// false when [EnvRoot] is set to the empty string.
func Root(lookup func(string) (string, bool)) (string, bool, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}

	if v, ok := lookup(EnvRoot); ok {
		v = strings.TrimSpace(v)
		if v == "" {
			return "", false, nil
		}
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", false, fmt.Errorf("resolve %s: %w", EnvRoot, err)
		}
		return abs, true, nil
	}

	if dir, _ := lookup("XDG_DATA_HOME"); strings.TrimSpace(dir) != "" {
		return filepath.Join(strings.TrimSpace(dir), "fft", "components"), true, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "fft", "components"), true, nil
}

// Registry is the set of components this fft can see.
//
// The zero value is usable and holds nothing, which is what a build with components
// disabled — or a machine with none installed — runs with. That matters: discovery
// happens while the command tree is being *built*, before any error could be
// reported to anyone, so a registry that cannot be opened must degrade to an empty
// one rather than fail.
type Registry struct {
	root string

	// firstParty is the compiled-in table, or whatever [WithFirstParty] replaced it
	// with.
	firstParty []Manifest

	// components is every component the registry knows: the first-party table,
	// merged with what was found on disk, sorted by name.
	components []Component

	// problems are the manifests that could not be read, kept rather than discarded
	// so that `fft component list` can show a broken install instead of silently
	// omitting it. Nothing else looks at them: a component fft cannot parse is a
	// component fft will not run.
	problems []Problem
}

// Problem is a directory under the root that is not a component fft can use.
type Problem struct {
	// Dir is the directory, absolute.
	Dir string

	// Err is what is wrong with it.
	Err error
}

// Open reads the component root.
//
// A missing root is not an error — it is what a machine with no components looks
// like, and creating it just to find it empty would put a directory in the user's
// home for a feature they have not used. A manifest that cannot be read is recorded
// as a [Problem] and skipped; one bad component must not take the CLI down with it.
func Open(root string, opts ...Option) *Registry {
	r := &Registry{root: root, firstParty: firstParty}
	for _, opt := range opts {
		opt(r)
	}
	r.components = firstPartyComponents(r.firstParty, root)

	if root == "" {
		return r
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			r.problems = append(r.problems, Problem{Dir: root, Err: err})
		}
		return r
	}

	known := make(map[string]bool, len(r.components))
	for _, c := range r.components {
		known[c.Name] = true
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, entry.Name())

		m, err := readManifest(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// A directory with no manifest is not a broken component, it is not a
				// component. Someone else's directory under a path they chose is theirs.
				continue
			}
			r.problems = append(r.problems, Problem{Dir: dir, Err: err})
			continue
		}
		if m.Name != entry.Name() {
			r.problems = append(r.problems, Problem{Dir: dir, Err: fmt.Errorf(
				"the manifest calls it %q but it is installed as %q", m.Name, entry.Name())})
			continue
		}

		if known[m.Name] {
			// A first-party component that is installed: the on-disk manifest supplies the
			// physical facts (which version, which binary) and the compiled-in table keeps
			// the command tree — see [FirstParty].
			r.hydrate(m, dir)
			continue
		}
		r.components = append(r.components, newComponent(m, dir, false))
	}

	sort.Slice(r.components, func(i, j int) bool { return r.components[i].Name < r.components[j].Name })
	return r
}

// hydrate fills in an installed first-party component from what is on disk.
func (r *Registry) hydrate(m Manifest, dir string) {
	for i, c := range r.components {
		if !c.FirstParty || c.Name != m.Name {
			continue
		}
		r.components[i].Version = m.Version
		r.components[i].Exec = m.Exec
		r.components[i].Dir = dir
		r.components[i].Installed = installed(dir, m.Exec)
		return
	}
}

// newComponent resolves a manifest and its directory into a component, settling the
// one question the manifest cannot answer: whether the binary is actually there.
func newComponent(m Manifest, dir string, firstParty bool) Component {
	return Component{
		Manifest:   m,
		Dir:        dir,
		FirstParty: firstParty,
		Installed:  installed(dir, m.Exec),
	}
}

// installed reports whether a component's executable exists.
//
// It accepts the manifest's spelling with the platform's suffix appended, so one
// manifest saying `bin/fft-emulator` is correct on Windows too — the release
// archive for that platform carries fft-emulator.exe, and requiring a per-platform
// manifest to say so would be a difference with no meaning behind it.
func installed(dir, exec string) bool {
	if dir == "" || exec == "" {
		return false
	}

	// isRegularFile Lstats, not Stats: a symlink standing in for the executable is
	// refused, the same rule ExecPath applies when it spawns it.
	path := filepath.Join(dir, filepath.FromSlash(exec))
	return slices.ContainsFunc([]string{path, execName(path)}, isRegularFile)
}

// readManifest reads and validates one component's manifest.
func readManifest(dir string) (Manifest, error) {
	path := filepath.Join(dir, ManifestName)

	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	return ParseManifest(data, path)
}

// Root is where this registry looks for components, and "" when they are disabled.
func (r *Registry) Root() string {
	if r == nil {
		return ""
	}
	return r.root
}

// All is every component the registry knows, sorted by name — installed or not,
// first-party or not.
func (r *Registry) All() []Component {
	if r == nil {
		return nil
	}
	return r.components
}

// Lookup finds a component by name.
func (r *Registry) Lookup(name string) (Component, bool) {
	if r == nil {
		return Component{}, false
	}
	for _, c := range r.components {
		if c.Name == name {
			return c, true
		}
	}
	return Component{}, false
}

// Transports are the installed transport components, which is what the emulator
// asks for when it works out where it can deliver an event.
func (r *Registry) Transports() []Component {
	if r == nil {
		return nil
	}

	var out []Component
	for _, c := range r.components {
		if c.Kind == KindTransport && c.Installed {
			out = append(out, c)
		}
	}
	return out
}

// Problems are the directories under the root that are not usable components.
func (r *Registry) Problems() []Problem {
	if r == nil {
		return nil
	}
	return r.problems
}

// Dir is where a component of this name would be installed. It does not have to
// exist.
func (r *Registry) Dir(name string) string {
	if r == nil || r.root == "" {
		return ""
	}
	return filepath.Join(r.root, name)
}
