package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
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

	dir, err := resolve(filepath.Join(abs, Name))
	if err != nil {
		return Plan{}, err
	}

	exists, err := recognise(dir)
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
	//
	// Asked of any directory that is there, not only one that already holds a skill:
	// a directory holding nothing but fft's own crash litter is one fft is about to
	// install into, and the litter should go with the same install rather than wait
	// to be complained about by the next one.
	if exists {
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

// resolve follows a symlinked skill directory to the directory it points at, and
// works on that instead.
//
// A symlinked ~/.claude/skills/fft is an ordinary dotfiles arrangement, and fft
// should install through it rather than refuse. What fft must never do is treat
// the *link* as one of the files it manages: a symlink is not a directory, so a
// walk of the tree reports it as a plain entry that the skill does not ship — and
// pruning would then delete the user's link, leaving the skill written correctly
// in a place nothing looks at any more. Resolving it here means everything below
// only ever sees a real directory.
func resolve(dir string) (string, error) {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return dir, nil
	case err != nil:
		return "", fmt.Errorf("stat %s: %w", dir, err)
	case info.Mode()&fs.ModeSymlink == 0:
		return dir, nil
	}

	target, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("follow %s: %w", dir, err)
	}
	return target, nil
}

// recognise reports whether dir is there at all, and refuses — with [ErrNotSkill]
// — a directory that is there and is not fft's.
//
// It answers "is there something to look at", not "is a skill installed": what the
// caller does with the answer is walk the directory for strays, and a directory
// holding nothing but fft's own crash litter has strays worth taking away.
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
		empty, err := vacant(dir)
		if err != nil {
			return false, err
		}
		if empty {
			return true, nil
		}
		return false, fmt.Errorf("%s holds files and no %s: %w", dir, Doc, ErrNotSkill)
	default:
		return false, fmt.Errorf("stat %s: %w", filepath.Join(dir, Doc), err)
	}
}

// vacant reports a directory with nothing of anybody's in it.
//
// fft's own litter does not count. A first install killed between the temporary
// file and the rename leaves a .tmp-* and no SKILL.md — and a directory holding
// only that would otherwise be "somebody else's" forever: install refuses it,
// --force never gets a say because the refusal comes first, and the way out is for
// the user to work out on their own that they must delete a file they have never
// heard of. fft made that file. It does not get to call it evidence of a stranger.
//
// The litter is still named in the plan and still removed with consent, like any
// other stray. This decides only whose directory it is.
func vacant(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", dir, err)
	}

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), atomicfile.TempPrefix) {
			return false, nil
		}
	}
	return true, nil
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
//
// Everything that is not shipped is a stray, litter from an interrupted write
// included. Nothing here is quietly swept up on the side: a file fft is going to
// remove is a file that appears in the plan, is shown to the user, and needs their
// consent — see [Plan.Apply].
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
		// The root itself, which a walk can only report as a file if it is not a
		// directory at all. [resolve] and [recognise] between them have already ruled
		// that out — but a "." here would name the skill's own directory as a file to
		// delete, and that is not a mistake to leave one refactor away from happening.
		if rel == "." {
			return nil
		}

		name := filepath.ToSlash(rel)
		if slices.Contains(ours, name) {
			return nil
		}

		// A stray is a file, and only a file. WalkDir does not follow a symlink, so it
		// reports a symlinked references/ as a plain entry that the skill does not ship
		// — and pruning it would delete the directory the reference files were just
		// written through, leaving a SKILL.md whose every link is dead. Which is the
		// same bug as the symlinked skill root, one level down: a link fft did not make
		// is not a file fft may remove. Following the link here is what tells the two
		// apart.
		info, err := os.Stat(p)
		if err != nil {
			// Dangling: it points at nothing, so there is nothing it could be shadowing,
			// and it is litter by any reading. Prune it with the rest.
			if errors.Is(err, fs.ErrNotExist) {
				out = append(out, name)
				return nil
			}
			return err
		}
		if info.IsDir() {
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
//
// fft's own crash litter is not the user's file, and asking them to consent to the
// removal of something they have never heard of and did not write teaches them
// only to say yes without reading. It is still in the plan, and still reported
// when it goes.
func (p Plan) Pending() []Change {
	var out []Change
	for _, c := range p.Files {
		if c.Status != StatusConflict && c.Status != StatusStale {
			continue
		}
		if litter(c.File) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// litter reports a file fft left behind itself: a temporary file from a write that
// was interrupted between creating it and renaming it over the target.
func litter(name string) bool {
	return strings.HasPrefix(path.Base(name), atomicfile.TempPrefix)
}

// Apply writes the skill. By the time it is called, whoever called it has already
// decided about [Plan.Pending] — so it overwrites and prunes without asking.
//
// It removes exactly what the plan named, and nothing else. That is the whole
// invariant: a file fft deletes is a file the user was shown and agreed to, so an
// install that reports no changes has made none. Anything swept up on the side —
// a stray file, an empty directory, fft's own litter — would be a deletion that
// happened without appearing in the plan that was consented to.
//
// An UNCHANGED file is not rewritten. Re-installing on every run of a script must
// not churn the mtime of a file an editor or a watcher is holding open.
func (p Plan) Apply() (Plan, error) {
	done := Plan{Dir: p.Dir, Files: make([]Change, 0, len(p.Files))}

	var emptied []string

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
			emptied = append(emptied, filepath.Dir(path))
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

	p.tidy(emptied)
	return done, nil
}

// tidy removes the directories that pruning emptied — and only those.
//
// It walks up from each pruned file towards the skill's own directory, stopping at
// the first directory that is not empty. os.Remove refuses a directory that still
// holds anything, which is the guard rather than a check: a directory with a file
// of the user's in it survives, and so does one fft never emptied.
func (p Plan) tidy(emptied []string) {
	for _, dir := range emptied {
		for dir != p.Dir && strings.HasPrefix(dir, p.Dir+string(filepath.Separator)) {
			if os.Remove(dir) != nil {
				break
			}
			dir = filepath.Dir(dir)
		}
	}
}
