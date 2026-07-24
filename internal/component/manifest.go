package component

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/config"
)

// Manifest is a component.yaml: what a component is, and what it adds to fft.
//
// It carries JSON tags as well as YAML ones because `fft component info -o json`
// renders it straight to the user.
type Manifest struct {
	// APIVersion is the contract the component was written against. See [APIVersion].
	APIVersion int `yaml:"apiVersion" json:"apiVersion"`

	// Name identifies the component and names its directory. It is also the default
	// command name, so it is constrained to what is safe as both — see [validName].
	Name string `yaml:"name" json:"name"`

	// Version is the component's own version, for `fft component list` and for
	// deciding whether an upgrade has anything to do. fft does not interpret it.
	Version string `yaml:"version" json:"version,omitempty"`

	// Description is one line, shown by `fft component list`.
	Description string `yaml:"description" json:"description,omitempty"`

	// Kind is what the component extends.
	Kind Kind `yaml:"kind" json:"kind"`

	// Source is where the component came from, recorded at install time so that
	// `fft component upgrade` knows where to look and `fft component info` can say
	// whose code this is.
	Source string `yaml:"source" json:"source,omitempty"`

	// Exec is the executable, as a slash-separated path relative to the component's
	// own directory. It may not escape it — see [validExec].
	Exec string `yaml:"exec" json:"exec"`

	// Env are the environment variables the component wants passed through from
	// fft's own environment, by name.
	//
	// It exists so a transport can keep reading the variable its ecosystem already
	// defines — PUBSUB_EMULATOR_HOST is the standard way to point a Pub/Sub client at
	// a local emulator, and a component should not have to learn an fft-specific
	// spelling of it. Everything not named here and not built by [Environ] is still
	// inherited; this list only covers the FFT_-prefixed names, which are stripped.
	Env []string `yaml:"env,omitempty" json:"env,omitempty"`

	// Targets are the subscription target types a transport component delivers, e.g.
	// GOOGLE_CLOUD_PUB_SUB. Only meaningful for [KindTransport].
	Targets []string `yaml:"targets,omitempty" json:"targets,omitempty"`

	// Commands are the commands a command component adds. Only meaningful for
	// [KindCommand].
	Commands []Command `yaml:"commands,omitempty" json:"commands,omitempty"`
}

// Command is one command a component adds to fft's tree.
type Command struct {
	// Name is the command as the user types it: `fft <name>`.
	Name string `yaml:"name" json:"name"`

	// Short is the one-line summary in `fft --help`.
	Short string `yaml:"short" json:"short"`

	// Long is the description `fft <name> --help` prints when the component is not
	// installed. Once it is, --help goes to the child, which knows its own flags.
	Long string `yaml:"long,omitempty" json:"long,omitempty"`

	// Session is how much of the caller's tenant session the command receives.
	Session Session `yaml:"session" json:"session"`

	// Mutates declares that the command can change the tenant. It is what the
	// read-only gate reads: a mutating component command is refused before the child
	// is spawned, exactly as a mutating operation is refused before a request is
	// built.
	//
	// It defaults to false, and that is safe *only* because a component that says
	// nothing also gets no credential to write with: SessionNone is the zero value
	// too. A manifest declaring a write session must declare Mutates as well, or
	// validation refuses it — see [validCommand].
	Mutates bool `yaml:"mutates,omitempty" json:"mutates"`

	// Claims are the operationIds this command supersedes. An operation a component
	// claims gets no generated Tier-2 twin, which is what lets the community promote
	// an endpoint to a curated UX the same way a hand-written command does.
	Claims []string `yaml:"claims,omitempty" json:"claims,omitempty"`
}

// nameRE is what a component and a command may be called: lowercase, starting with
// a letter, and made of letters, digits and single dashes.
//
// It is strict on purpose. The name is used as a directory name under the component
// root and as a command name in the tree, so anything it lets through has to be
// harmless as both — no separators, no dots, no leading dash that argument parsing
// would read as a flag.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ParseManifest reads a component.yaml and checks it.
//
// source names the file in error messages; it is the path the manifest was read
// from, or a description of where it came from.
func ParseManifest(data []byte, source string) (Manifest, error) {
	var m Manifest

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	// An unknown field is a manifest written against a contract this build does not
	// have, and the version check below cannot catch it on its own: a component that
	// adds a field without bumping apiVersion would otherwise run with that field
	// silently dropped.
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("read %s: %w", source, err)
	}

	if err := m.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", source, err)
	}
	return m, nil
}

// Validate reports what is wrong with a manifest, in terms its author can act on.
func (m Manifest) Validate() error {
	if m.APIVersion != APIVersion {
		return fmt.Errorf(
			"apiVersion %d is not the contract this fft speaks (%d): upgrade fft, or the component",
			m.APIVersion, APIVersion)
	}
	if !nameRE.MatchString(m.Name) {
		return fmt.Errorf("name %q must be lowercase letters, digits and dashes, starting with a letter", m.Name)
	}
	if err := validExec(m.Exec); err != nil {
		return err
	}

	switch m.Kind {
	case KindCommand:
		if len(m.Commands) == 0 {
			return fmt.Errorf("kind %q declares no commands, so it would add nothing", KindCommand)
		}
		if len(m.Targets) > 0 {
			return fmt.Errorf("kind %q cannot declare targets: only a %q component delivers events", KindCommand, KindTransport)
		}
		seen := make(map[string]bool, len(m.Commands))
		for _, c := range m.Commands {
			if err := validCommand(c); err != nil {
				return err
			}
			if seen[c.Name] {
				return fmt.Errorf("command %q is declared twice", c.Name)
			}
			seen[c.Name] = true
		}

	case KindTransport:
		if len(m.Targets) == 0 {
			return fmt.Errorf("kind %q declares no targets, so nothing would ever be delivered to it", KindTransport)
		}
		if len(m.Commands) > 0 {
			return fmt.Errorf("kind %q cannot declare commands: it is addressed by the emulator, not by the user", KindTransport)
		}

	default:
		return fmt.Errorf("unknown kind %q: want %s or %s", m.Kind, KindCommand, KindTransport)
	}

	for _, name := range m.Env {
		if strings.HasPrefix(name, "FFT_") && !forwardable[name] {
			// The FFT_ namespace is fft's to hand over, and the session level decides what
			// of it a component sees. A manifest that could ask for FFT_PASSWORD by name
			// would make that decision negotiable by the component being decided about.
			return fmt.Errorf("env %q: a component may ask for %s and no other FFT_ variable; the session level decides the rest",
				name, strings.Join(forwardableNames(), ", "))
		}
	}
	return nil
}

// forwardable are the FFT_ variables a component may ask for by name, whatever
// session it declares.
//
// It has exactly one member, and adding a second should be hard. What qualifies is a
// variable that is not a credential and does not describe one: the base URL says
// *where*, never *who*, and a component that has it still cannot authenticate.
//
// The emulator is why it exists. `fft emulator emit` talks to a running emulator, and
// the address it talks to is the one the emulator's own startup recipe exports — so
// the component that needs FFT_BASE_URL here needs it to reach a fake tenant it is
// itself serving, which is the opposite of the case the stripping protects against.
var forwardable = map[string]bool{config.EnvBaseURL: true}

func forwardableNames() []string {
	names := make([]string, 0, len(forwardable))
	for name := range forwardable {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// validCommand checks one declared command.
func validCommand(c Command) error {
	if !nameRE.MatchString(c.Name) {
		return fmt.Errorf("command name %q must be lowercase letters, digits and dashes, starting with a letter", c.Name)
	}
	if strings.TrimSpace(c.Short) == "" {
		return fmt.Errorf("command %q has no short description, so `fft --help` would list it blank", c.Name)
	}

	switch c.Session {
	case SessionNone, SessionRead:
		if c.Mutates {
			// Not a contradiction fft can resolve by picking one. A command that writes
			// needs a session that can, and one that declares it writes while asking for a
			// read-only session would be refused by the tenant at the worst moment.
			return fmt.Errorf("command %q declares mutates with session %q: a write needs session %q",
				c.Name, c.Session, SessionWrite)
		}
	case SessionWrite:
		if !c.Mutates {
			// The read-only gate reads Mutates, not Session. A command holding a writable
			// session while declaring it writes nothing would sail through a gate that
			// exists precisely to stop it.
			return fmt.Errorf("command %q asks for session %q but does not declare mutates: "+
				"the read-only gate reads mutates, so it would not be gated", c.Name, SessionWrite)
		}
	default:
		return fmt.Errorf("command %q has unknown session %q: want %s, %s or %s",
			c.Name, c.Session, SessionNone, SessionRead, SessionWrite)
	}
	return nil
}

// validExec refuses an executable path that leaves the component's own directory.
//
// A manifest is data fft downloaded, and this is the line between "run the binary
// that came with it" and "run whatever it names". An absolute path or a `..` would
// turn `fft component install` into a way to execute a file already on the machine,
// chosen by whoever wrote the manifest.
func validExec(exec string) error {
	switch {
	case exec == "":
		return fmt.Errorf("exec is empty: a component must say which binary to run")
	case path.IsAbs(exec) || strings.HasPrefix(exec, "/") || strings.Contains(exec, `\`):
		return fmt.Errorf("exec %q must be a relative, slash-separated path inside the component's directory", exec)
	}

	clean := path.Clean(exec)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("exec %q leaves the component's directory", exec)
	}
	return nil
}
