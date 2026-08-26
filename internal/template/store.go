package template

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// Scope is where a template lives.
//
// The two are not a hierarchy of importance, they are a hierarchy of audience: a
// project template belongs to whoever clones the repository, and a user template
// belongs to one person on one machine.
type Scope string

const (
	// ScopeProject is ./.fft/templates: committed, shared with the team.
	ScopeProject Scope = "project"

	// ScopeUser is $XDG_DATA_HOME/fft/templates: personal, this machine only.
	ScopeUser Scope = "user"
)

// ext is the extension every template file carries. Anything else in the
// directory is somebody else's file and is left alone.
const ext = ".json"

// maxTemplateBytes bounds a template file. A project template arrives via git
// clone — untrusted input — and a real request body never approaches this size;
// a file that does is already unreadable to the human this design relies on
// reading it before trusting it, and reading it in full regardless is a cheap
// way for a committed file to cost every future `list`/`show`/`render` real CPU.
const maxTemplateBytes = 4 << 20 // 4 MiB

// projectDirName is the repository-local directory templates live under. It sits
// beside .git rather than inside .claude or .config because it is a project
// artifact: the point of the scope is that it gets committed.
const projectDirName = ".fft"

// Directory modes. A user template is data about a tenant and gets the 0700 the
// rest of fft's private state gets. A project template lives in a working tree
// among directories other tools traverse, so its directory is 0755 — the file
// inside is still 0600, because nothing is gained by widening it before git does.
const (
	userDirMode    = atomicfile.DirMode
	projectDirMode = 0o755
)

// maxNameLen bounds a template name. It is a filename, and a name nobody can
// read back to a colleague is not doing its job anyway.
const maxNameLen = 64

// windowsReservedStems are the DOS device names Windows still resolves ahead of
// a real file, in every directory and whatever the extension: on the Windows
// binaries fft releases, opening templates\nul.json opens the null device, so
// `fft template save nul` reports success and stores nothing, and every later
// read of it says the template is not there.
//
// A name is refused on every platform rather than only on Windows. A project
// template is committed and cloned, and one a colleague cannot use is worse than
// one nobody could save in the first place — where the error names the reason.
var windowsReservedStems = []string{
	"con", "prn", "aux", "nul",
	"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
	"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9",
}

// ValidateName refuses a name that cannot safely become a filename.
//
// This is the path-traversal guard, and it runs before any path is joined:
// `fft template remove ../../.ssh/id_rsa` has to fail on the name, because by
// the time it is a path the damage is one os.Remove away. Refusing a leading dot
// takes "." and ".." with it.
func ValidateName(name string) error {
	if name == "" {
		return exitcode.UsageError{Err: errors.New("a template name cannot be empty")}
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return exitcode.UsageError{Err: fmt.Errorf(
			"a template name can be at most %d characters, and %q is %d",
			maxNameLen, name, utf8.RuneCountInString(name))}
	}

	for i, r := range name {
		ok := r == '-' || r == '_' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return exitcode.UsageError{Err: fmt.Errorf(
				"a template name may only hold letters, digits, '-', '_' and '.', and %q holds %q",
				name, string(r))}
		}
		if i == 0 && r == '.' {
			return exitcode.UsageError{Err: fmt.Errorf(
				"a template name cannot start with a dot, and %q does", name)}
		}
	}

	// Windows reads a device name off the part before the first dot, so nul.json
	// and nul.backup.json are both the null device — the stem is what has to be
	// checked, not the whole name.
	if stem, _, _ := strings.Cut(strings.ToLower(name), "."); slices.Contains(windowsReservedStems, stem) {
		return exitcode.UsageError{Err: fmt.Errorf(
			"%q is a reserved device name on Windows, where %q would open the device instead of a file",
			name, name+ext)}
	}
	return nil
}

// UserDir is $XDG_DATA_HOME/fft/templates, else ~/.local/share/fft/templates.
//
// Data, like the components directory and unlike the config: a template is
// neither something fft can regenerate by asking again nor something it manages
// on the user's behalf.
func UserDir(lookup func(string) (string, bool)) (string, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if dir, _ := lookup("XDG_DATA_HOME"); strings.TrimSpace(dir) != "" {
		return filepath.Join(strings.TrimSpace(dir), "fft", "templates"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "fft", "templates"), nil
}

// ProjectDir is ./.fft/templates: the templates for one repository, which the
// team shares by committing them.
//
// It is the working directory's, not an ancestor's. Walking up would find a
// template from a repository the user has merely wandered into, and a body that
// changes stock is not something to inherit by accident.
func ProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locate the current directory: %w", err)
	}
	return filepath.Join(wd, projectDirName, "templates"), nil
}

// Store is the two directories fft looks in, project first.
type Store struct {
	project string
	user    string
}

// NewStore resolves both scopes. lookup reads the environment; nil means the
// real one.
func NewStore(lookup func(string) (string, bool)) (*Store, error) {
	project, err := ProjectDir()
	if err != nil {
		return nil, err
	}
	user, err := UserDir(lookup)
	if err != nil {
		return nil, err
	}
	return &Store{project: project, user: user}, nil
}

// Dir is where templates of the given scope live.
func (s *Store) Dir(scope Scope) string {
	if scope == ScopeProject {
		return s.project
	}
	return s.user
}

// Path is where the named template of the given scope lives, whether or not it
// is there.
func (s *Store) Path(name string, scope Scope) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.Dir(scope), name+ext), nil
}

// Saved is a template and where it was found. Resolve and List return it, and
// it is what `fft template show`'s table form renders. Under -o json/yaml,
// show instead renders the template file's own encoded bytes — not this
// struct — so that `show -o json` round-trips through `save --file -`.
type Saved struct {
	Name  string `json:"name"`
	Scope Scope  `json:"scope"`
	Path  string `json:"path"`

	*Template
}

// Problem is a file in a template directory that fft could not read.
//
// It is reported rather than returned as an error because one unreadable file
// must not make `fft template list` useless — the same reasoning the component
// registry uses for a component it cannot load.
type Problem struct {
	Path string
	Err  error
}

// Listing is what the store holds: the templates a name resolves to, the user
// templates a project template hides, and the files that would not parse.
type Listing struct {
	Found    []Saved
	Shadowed []Saved
	Problems []Problem
}

// Load reads one template from one scope.
func (s *Store) Load(name string, scope Scope) (Saved, error) {
	path, err := s.Path(name, scope)
	if err != nil {
		return Saved{}, err
	}
	return read(path, name, scope)
}

// Resolve finds a template by name, preferring the project scope.
//
// Project first because that is the more specific answer: someone who committed
// a template to this repository meant it to be the one this repository uses.
func (s *Store) Resolve(name string) (Saved, error) {
	if err := ValidateName(name); err != nil {
		return Saved{}, err
	}

	for _, scope := range []Scope{ScopeProject, ScopeUser} {
		saved, err := s.Load(name, scope)
		switch {
		case err == nil:
			return saved, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return Saved{}, err
		}
	}

	return Saved{}, &NotFoundError{Name: name, Known: s.names()}
}

// List reads both scopes.
func (s *Store) List() (Listing, error) {
	var out Listing

	seen := make(map[string]bool)
	for _, scope := range []Scope{ScopeProject, ScopeUser} {
		names, err := s.scan(scope)
		if err != nil {
			return Listing{}, err
		}

		for _, name := range names {
			saved, err := s.Load(name, scope)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				path, _ := s.Path(name, scope)
				out.Problems = append(out.Problems, Problem{Path: path, Err: err})
				continue
			}

			if seen[name] {
				out.Shadowed = append(out.Shadowed, saved)
				continue
			}
			seen[name] = true
			out.Found = append(out.Found, saved)
		}
	}

	slices.SortFunc(out.Found, func(a, b Saved) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(out.Shadowed, func(a, b Saved) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Write saves a template, creating the directory if it is not there yet.
//
// The file is 0600 in both scopes. A project template is meant to be shared, but
// it is git that shares it; widening the mode on this machine first would only
// widen it for whoever else is logged into this machine.
func (s *Store) Write(name string, scope Scope, t *Template) (string, error) {
	path, err := s.Path(name, scope)
	if err != nil {
		return "", err
	}

	data, err := Encode(t)
	if err != nil {
		return "", err
	}

	dirMode := os.FileMode(userDirMode)
	if scope == ScopeProject {
		dirMode = projectDirMode
	}
	if err := atomicfile.WriteMode(path, data, atomicfile.FileMode, dirMode); err != nil {
		return "", err
	}
	return path, nil
}

// Remove deletes a template and reports the file it deleted.
func (s *Store) Remove(name string, scope Scope) (string, error) {
	path, err := s.Path(name, scope)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", &NotFoundError{Name: name, Known: s.names()}
		}
		return "", fmt.Errorf("remove %s: %w", path, err)
	}
	return path, nil
}

// Exists reports whether the named template is already in the given scope.
func (s *Store) Exists(name string, scope Scope) (bool, error) {
	path, err := s.Path(name, scope)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("look for %s: %w", path, err)
	}
}

// scan lists the template names in one scope. A directory that is not there is
// an empty scope, not a failure: neither one has to exist for fft to work.
func (s *Store) scan(scope Scope) ([]string, error) {
	entries, err := os.ReadDir(s.Dir(scope))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", s.Dir(scope), err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ext)
		if ValidateName(name) != nil {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// names is every template name the store can see, for a "did you mean".
func (s *Store) names() []string {
	var out []string
	for _, scope := range []Scope{ScopeProject, ScopeUser} {
		names, err := s.scan(scope)
		if err != nil {
			continue
		}
		out = append(out, names...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// read loads and decodes one template file.
func read(path, name string, scope Scope) (Saved, error) {
	if info, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Saved{}, err
		}
		return Saved{}, fmt.Errorf("read %s: %w", path, err)
	} else if info.Size() > maxTemplateBytes {
		return Saved{}, fmt.Errorf("%s is %d bytes, more than the %d fft reads as a template",
			path, info.Size(), maxTemplateBytes)
	}

	// The path is a template name ValidateName has already refused a separator,
	// a leading dot and everything outside [A-Za-z0-9._-] in, joined onto a
	// directory fft chose. That is the guard; gosec cannot see it.
	data, err := os.ReadFile(path) //nolint:gosec // path is a validated name under a directory fft owns
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Saved{}, err
		}
		return Saved{}, fmt.Errorf("read %s: %w", path, err)
	}

	t, err := Decode(data)
	if err != nil {
		return Saved{}, fmt.Errorf("%s: %w", path, err)
	}
	return Saved{Name: name, Scope: scope, Path: path, Template: t}, nil
}

// NotFoundError is a template nobody saved. It exits 6, like any other thing the
// user named that is not there.
type NotFoundError struct {
	Name string

	// Known is every template fft can see, for the hint.
	Known []string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no template named %q", e.Name)
}

// ExitCode implements the interface exitcode.FromError looks for.
func (e *NotFoundError) ExitCode() int { return exitcode.NotFound }

// Hint names the templates that do exist, which is nearly always enough to spot
// the typo without a second command.
func (e *NotFoundError) Hint() string {
	if len(e.Known) == 0 {
		return "There are no templates yet. Run 'fft template save <name> --file body.json' to add one."
	}
	return "Saved templates: " + strings.Join(e.Known, ", ") + "."
}
