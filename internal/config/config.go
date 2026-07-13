// Package config holds fft's project model and its YAML persistence.
//
// A project is one fulfillmenttools tenant plus the Firebase project that
// authenticates against it. Users configure a project once and switch between
// them with `fft project use`.
//
// # Why not viper
//
// Viper is the natural reach for a CLI config file, and it is the wrong tool
// here: it lower-cases every key on both read and write. `activeProject` comes
// back as `activeproject`, `baseUrl` as `baseurl`, and a load-then-save cycle
// silently rewrites the user's file into keys nothing reads. So the file is
// persisted with gopkg.in/yaml.v3 into the typed structs below, and viper is
// used only for what it is good at: the flag → env → default precedence chain
// on the global flags.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// Version is the schema version written to new config files. It exists so that
// a future breaking change to the file layout can be detected and migrated
// rather than misread.
const Version = 1

// Output formats a project may default to.
const (
	OutputTable = "table"
	OutputJSON  = "json"
	OutputYAML  = "yaml"
)

// Config is the whole of ~/.config/fft/config.yaml.
type Config struct {
	Version       int       `yaml:"version"`
	ActiveProject string    `yaml:"activeProject,omitempty"`
	Projects      []Project `yaml:"projects,omitempty"`
	Settings      Settings  `yaml:"settings"`
}

// Settings are the preferences that apply across all projects.
type Settings struct {
	// Output is the default output format when -o is not given.
	Output string `yaml:"output"`
	// UpdateCheck enables the once-a-day check for a newer fft release.
	UpdateCheck bool `yaml:"updateCheck"`
}

// Project is one configured fulfillmenttools tenant. It holds no secrets: the
// password and tokens live in a [secrets.Store].
type Project struct {
	// Name identifies the project to the user and namespaces its secrets.
	Name string `yaml:"name"`

	// BaseURL is the fully-qualified API root, for example
	// "https://acme.api.fulfillmenttools.com". It is stored, never derived.
	//
	// The official docs contradict themselves on whether the host is
	// "{projectId}.api…" or "ocff-{projectId}.api…", so fft refuses to guess:
	// ProjectID and Environment below exist for display and for building a
	// candidate email, and are never used to construct a URL.
	BaseURL string `yaml:"baseUrl"`

	// FirebaseAPIKey is the Firebase *Web* API key. It identifies the Firebase
	// project and confers no authorization by itself; it is sent only as the
	// ?key= parameter on Google's identity endpoints, never to fulfillmenttools.
	FirebaseAPIKey string `yaml:"firebaseApiKey"`

	// Email is the address that actually authenticates. fulfillmenttools users
	// often have a synthetic address ({username}@ocff-{projectId}-{env}.com) but
	// not always, so whatever value signs in successfully is persisted verbatim
	// rather than recomputed.
	Email string `yaml:"email"`

	// Username is the short login name the email was derived from, kept so that
	// `fft project list` can show something a human recognises.
	Username string `yaml:"username,omitempty"`

	// Tenant, ProjectID and Environment are descriptive only.
	Tenant      string `yaml:"tenant,omitempty"`
	ProjectID   string `yaml:"projectId,omitempty"`
	Environment string `yaml:"environment,omitempty"`

	// ReadOnly refuses every request that would change this tenant. The project
	// still gets, lists and searches — including the POST-bodied cursor searches,
	// which are reads — and a refused write is refused before fft signs in, so it
	// costs no round trip and mints no token.
	//
	// It protects the *tenant*, not the config file: `fft project use` and
	// `fft project remove` still work on a read-only project, because configuring
	// fft is not changing anything in the tenant.
	//
	// omitempty, so that adding this field churns no existing config file.
	ReadOnly bool `yaml:"readOnly,omitempty"`

	// Ephemeral marks a project synthesized from FFT_* environment variables in
	// headless mode. It is never written to disk — hence the yaml:"-".
	Ephemeral bool `yaml:"-"`
}

// New returns a Config with the defaults a fresh install should have.
func New() *Config {
	return &Config{
		Version:  Version,
		Settings: Settings{Output: OutputTable, UpdateCheck: true},
	}
}

// Error is a configuration problem: no active project, an unknown project name,
// an unreadable file. It exits 3 and carries a hint telling the user what to run
// next, which is the difference between an error a user can act on and one they
// have to go and read the manual about.
type Error struct {
	Err  error
	hint string
}

// NewError wraps err as a configuration problem, exiting 3, with a hint telling
// the user which command fixes it. hint may be empty.
func NewError(err error, hint string) *Error {
	return &Error{Err: err, hint: hint}
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ExitCode implements the interface exitcode.FromError looks for.
func (e *Error) ExitCode() int { return exitcode.Config }

// Hint returns the suggested next command, or "" if there is none.
func (e *Error) Hint() string { return e.hint }

// Sentinels callers branch on. They are always wrapped in an [Error], so they
// carry exit code 3 with them.
var (
	// ErrNoActiveProject means nothing told fft which project to use.
	ErrNoActiveProject = errors.New("no active project")
	// ErrProjectNotFound means a project was named but is not configured.
	ErrProjectNotFound = errors.New("project not found")
)

// Find returns the project with the given name.
func (c *Config) Find(name string) (Project, bool) {
	i := slices.IndexFunc(c.Projects, func(p Project) bool { return p.Name == name })
	if i < 0 {
		return Project{}, false
	}
	return c.Projects[i], true
}

// Upsert adds the project, replacing any existing one with the same name.
func (c *Config) Upsert(p Project) {
	i := slices.IndexFunc(c.Projects, func(q Project) bool { return q.Name == p.Name })
	if i < 0 {
		c.Projects = append(c.Projects, p)
		return
	}
	c.Projects[i] = p
}

// Remove deletes the named project and, if it was the active one, clears the
// active selection. It reports whether the project existed.
func (c *Config) Remove(name string) bool {
	before := len(c.Projects)
	c.Projects = slices.DeleteFunc(c.Projects, func(p Project) bool { return p.Name == name })
	if len(c.Projects) == before {
		return false
	}

	if c.ActiveProject == name {
		c.ActiveProject = ""
	}
	return true
}

// Resolve picks the project a command should act on.
//
// The order is: an explicit name (--project, or FFT_PROJECT — viper has already
// collapsed those two by the time we get here), then the config file's
// activeProject. Anything else is an [Error] at exit code 3.
func (c *Config) Resolve(name string) (Project, error) {
	if name == "" {
		name = c.ActiveProject
	}
	if name == "" {
		return Project{}, &Error{
			Err:  ErrNoActiveProject,
			hint: "Run 'fft project add <name>' to configure one, or 'fft project use <name>' to select an existing one.",
		}
	}

	p, ok := c.Find(name)
	if !ok {
		return Project{}, &Error{
			Err:  fmt.Errorf("%w: %q", ErrProjectNotFound, name),
			hint: c.notFoundHint(),
		}
	}
	return p, nil
}

func (c *Config) notFoundHint() string {
	if len(c.Projects) == 0 {
		return "No projects are configured. Run 'fft project add <name>'."
	}

	names := make([]string, 0, len(c.Projects))
	for _, p := range c.Projects {
		names = append(names, p.Name)
	}
	return "Configured projects: " + strings.Join(names, ", ") + ". Run 'fft project list' to see them."
}

// CandidateEmail builds the synthetic address fulfillmenttools issues for a
// username: {username}@ocff-{projectId}-{env}.com.
//
// It is a *candidate*, not the truth. Some tenants authenticate with a plain
// corporate address instead, so this only seeds the sign-in attempt; whatever
// actually works is what gets stored in [Project.Email]. It returns "" when it
// has too little to work with, rather than a plausible-looking wrong answer.
func CandidateEmail(username, projectID, environment string) string {
	if username == "" || projectID == "" || environment == "" {
		return ""
	}
	if strings.Contains(username, "@") {
		return username
	}
	return fmt.Sprintf("%s@ocff-%s-%s.com", username, projectID, environment)
}

// Store persists a Config as YAML.
type Store struct {
	path string
}

// NewStore returns a Store reading and writing the file at path.
func NewStore(path string) *Store { return &Store{path: path} }

// Path is the file the store reads and writes.
func (s *Store) Path() string { return s.path }

// DefaultPath is $XDG_CONFIG_HOME/fft/config.yaml, falling back to
// ~/.config/fft/config.yaml.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "fft", "config.yaml"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, ".config", "fft", "config.yaml"), nil
}

// Load reads the config file. A missing file is not an error: a first run has
// no config yet, and every command that needs a project will fail with a useful
// hint soon enough.
func (s *Store) Load() (*Config, error) {
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return New(), nil
	case err != nil:
		return nil, &Error{
			Err:  fmt.Errorf("read %s: %w", s.path, err),
			hint: "Check the file's permissions, or delete it to start over.",
		}
	}

	cfg := New()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, &Error{
			Err:  fmt.Errorf("parse %s: %w", s.path, err),
			hint: "Fix the YAML by hand, or delete the file and re-run 'fft project add'.",
		}
	}

	if cfg.Version > Version {
		return nil, &Error{
			Err:  fmt.Errorf("%s was written by a newer fft (config version %d, this build understands %d)", s.path, cfg.Version, Version),
			hint: "Upgrade fft.",
		}
	}
	return cfg, nil
}

// Save writes the config atomically, mode 0600.
func (s *Store) Save(cfg *Config) error {
	if cfg.Version == 0 {
		cfg.Version = Version
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode the config: %w", err)
	}
	if err := atomicfile.Write(s.path, data); err != nil {
		return fmt.Errorf("save %s: %w", s.path, err)
	}
	return nil
}
