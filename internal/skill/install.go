package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
)

// Documentation, not credentials: atomicfile's own 0600/0700 would be a claim
// about these files that is not true, and would keep the user's editor — or their
// agent, running as another user on a shared box — from reading a document they
// meant to publish to it. What the skill does want from atomicfile is the rest of
// it: a SKILL.md truncated by a full disk is a skill that lies, which is the one
// outcome worth engineering against.
const (
	fileMode = 0o644
	dirMode  = 0o755
)

// The states a file of the skill can be in, on the way to being installed.
//
// The distinction that earns its keep is UNCHANGED against CONFLICT. Installing
// twice must be silent and must ask nothing — an agent, or a provisioning script,
// will do it on every run — while a file the *user* has edited is a decision only
// they can make. Comparing the bytes is what tells those apart; a timestamp or a
// version marker would not.
type Status string

const (
	StatusNew       Status = "NEW"       // nothing is there
	StatusUnchanged Status = "UNCHANGED" // already byte-for-byte what fft ships
	StatusConflict  Status = "CONFLICT"  // something else is there; --force replaces it
	StatusStale     Status = "STALE"     // fft does not ship this any more; --force removes it
	StatusWritten   Status = "WRITTEN"   // Apply created it
	StatusReplaced  Status = "REPLACED"  // Apply overwrote a CONFLICT
	StatusRemoved   Status = "REMOVED"   // Apply pruned a STALE
)

// Change is what installing does to one file. It carries its own tags because it
// is rendered straight to the user under -o json.
type Change struct {
	// File is the path within the skill: "SKILL.md", "references/recipes.md".
	File string `json:"file" yaml:"file"`

	Status Status `json:"status" yaml:"status"`
}

// ErrNotSkill reports a directory that fft did not put there.
//
// It is the whole reason --force is safe to offer. --force means "replace my copy
// of the skill", and the directory it replaces can come from --dir — from a shell,
// where a typo is one keystroke. A directory with no SKILL.md in it is somebody
// else's, and fft will not remove a single file from it.
var ErrNotSkill = errors.New("not an fft skill directory")

// Plan is what installing into a root directory would do. Building one reads; it
// writes nothing, so the user can be asked before anything happens to their disk.
type Plan struct {
	// Dir is the skill's own directory — <root>/fft — absolute, so that what is
	// printed is a path the user can act on.
	Dir string

	Files []Change
}

// NewPlan works out what installing the skill under root would do.
//
// It refuses a directory that exists and holds no SKILL.md: see [ErrNotSkill].
func NewPlan(root string) (Plan, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve %s: %w", root, err)
	}
	dir := filepath.Join(abs, Name)

	installed, err := recognise(dir)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{Dir: dir}

	ours := make([]string, 0, 8)
	err = fs.WalkDir(tree, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ours = append(ours, name)

		plan.Files = append(plan.Files, Change{File: name, Status: compare(dir, name)})
		return nil
	})
	if err != nil {
		return Plan{}, err
	}

	// Files fft used to ship and does not any more. A reference file dropped in a
	// later release would otherwise outlive the release, sitting in the user's home
	// telling their agent about a command that no longer exists — and an agent has
	// no way to know which of two documents is the stale one.
	if installed {
		stale, err := strays(dir, ours)
		if err != nil {
			return Plan{}, err
		}
		for _, name := range stale {
			plan.Files = append(plan.Files, Change{File: name, Status: StatusStale})
		}
	}

	// SKILL.md first, then the rest in walk order, then the strays.
	slices.SortStableFunc(plan.Files, func(a, b Change) int {
		return entry(a) - entry(b)
	})
	return plan, nil
}

// entry ranks a change for display: SKILL.md, then the files fft ships, then the
// ones it is about to take away.
func entry(c Change) int {
	switch {
	case c.File == Doc:
		return 0
	case c.Status == StatusStale:
		return 2
	default:
		return 1
	}
}

// recognise reports whether dir already holds this skill, and refuses to say yes
// about a directory that is not one.
func recognise(dir string) (bool, error) {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("stat %s: %w", dir, err)
	case !info.IsDir():
		return false, fmt.Errorf("%s is a file, not a directory: %w", dir, ErrNotSkill)
	}

	switch _, err := os.Stat(filepath.Join(dir, Doc)); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		// An empty directory is nobody's, and installing into it is the friendly
		// answer — a user who ran `mkdir -p ~/.claude/skills/fft` first should not be
		// told off for it.
		empty, err := isEmpty(dir)
		if err != nil {
			return false, err
		}
		if empty {
			return false, nil
		}
		return false, fmt.Errorf("%s holds files and no %s: %w", dir, Doc, ErrNotSkill)
	default:
		return false, fmt.Errorf("stat %s: %w", filepath.Join(dir, Doc), err)
	}
}

func isEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", dir, err)
	}
	return len(entries) == 0, nil
}

// compare says what is at dir/name against what fft ships there.
//
// An unreadable file is a CONFLICT rather than an error: fft is about to overwrite
// it anyway, and if the overwrite fails the user hears about it then, with the
// error that actually stopped it.
func compare(dir, name string) Status {
	want, err := fs.ReadFile(tree, name)
	if err != nil {
		panic(fmt.Sprintf("embedded skill: %v", err))
	}

	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return StatusNew
	case err != nil:
		return StatusConflict
	case bytes.Equal(got, want):
		return StatusUnchanged
	default:
		return StatusConflict
	}
}

// strays are the files under dir that fft does not ship, sorted.
func strays(dir string, ours []string) ([]string, error) {
	var out []string

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)

		// A .tmp-* is an interrupted atomicfile write's litter rather than a document
		// anybody wrote. It is swept up by Apply, but it is not reported: telling the
		// user that fft removed a file fft created would explain nothing.
		if slices.Contains(ours, name) || strings.HasPrefix(filepath.Base(name), ".tmp-") {
			return nil
		}
		out = append(out, name)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	slices.Sort(out)
	return out, nil
}

// Pending are the changes that need the user's blessing: a file of theirs to be
// overwritten, or one to be removed. Everything else — a new file, a file already
// correct — needs nobody's permission.
func (p Plan) Pending() []Change {
	var out []Change
	for _, c := range p.Files {
		if c.Status == StatusConflict || c.Status == StatusStale {
			out = append(out, c)
		}
	}
	return out
}

// Apply writes the skill. By the time it is called, whoever called it has already
// decided about [Plan.Pending] — so it overwrites and prunes without asking.
//
// An UNCHANGED file is not rewritten. Re-installing on every run of a script must
// not churn the mtime of a file an editor or a watcher is holding open.
func (p Plan) Apply() (Plan, error) {
	done := Plan{Dir: p.Dir, Files: make([]Change, 0, len(p.Files))}

	for _, c := range p.Files {
		path := filepath.Join(p.Dir, filepath.FromSlash(c.File))

		switch c.Status {
		case StatusUnchanged:
			done.Files = append(done.Files, c)
			continue

		case StatusStale:
			if err := os.Remove(path); err != nil {
				return Plan{}, fmt.Errorf("remove %s: %w", path, err)
			}
			done.Files = append(done.Files, Change{File: c.File, Status: StatusRemoved})
			continue
		}

		data, err := fs.ReadFile(tree, c.File)
		if err != nil {
			return Plan{}, fmt.Errorf("read the embedded %s: %w", c.File, err)
		}
		if err := atomicfile.WriteMode(path, data, fileMode, dirMode); err != nil {
			return Plan{}, err
		}

		status := StatusWritten
		if c.Status == StatusConflict {
			status = StatusReplaced
		}
		done.Files = append(done.Files, Change{File: c.File, Status: status})
	}

	if err := p.sweep(); err != nil {
		return Plan{}, err
	}
	return done, nil
}

// sweep removes the directories the pruning emptied, and the litter of an
// interrupted write.
//
// os.Remove refuses a directory that is not empty, which is exactly the guard
// wanted: a directory still holding anything at all survives, whatever fft thinks
// of its contents.
func (p Plan) sweep() error {
	var dirs, litter []string

	// The walk only looks. Removing a path from inside a WalkDir callback races the
	// walk itself against anything else touching the tree, so the two are kept apart:
	// decide here, act below.
	err := filepath.WalkDir(p.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if path != p.Dir {
				dirs = append(dirs, path)
			}
		case strings.HasPrefix(d.Name(), ".tmp-"):
			litter = append(litter, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("sweep %s: %w", p.Dir, err)
	}

	for _, path := range litter {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("sweep %s: %w", p.Dir, err)
		}
	}

	// Deepest first, so a directory emptied by the removals above is empty by the
	// time it is tried.
	slices.Reverse(dirs)
	for _, dir := range dirs {
		_ = os.Remove(dir)
	}
	return nil
}
