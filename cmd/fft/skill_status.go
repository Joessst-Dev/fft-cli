package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/skill"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

const skillStatusLong = `Report which version of fft each installed skill describes.

An installed skill is a copy, and it describes the fft that installed it — not the
one you are running now. With no flags this looks in both places Claude Code reads a
skill from, ~/.claude/skills/fft and ./.claude/skills/fft; --local and --dir look in
one.

  CURRENT     what this fft ships
  OUTDATED    installed by another fft version: run fft skill install
  MODIFIED    this version, with files edited or added since: --force puts it back
  MISSING     nothing installed there
  NOT_SKILL   the directory holds files that are not fft's skill
  UNREADABLE  fft could not look: the reason is on stderr, and in "error" under -o json

A build that did not come from a release tag has no version to compare, and
counts every skill as current.

It only reads the installed files, sends nothing, and exits 0 whatever it finds:
it is a report, and a skill that is outdated or cannot be read is not a failure of
this command.`

// The state of the skill in one location, as `fft skill status` reports it. These
// summarise a whole install; [skill.Status] is the state of one file in it.
type skillState string

const (
	skillCurrent    skillState = "CURRENT"
	skillOutdated   skillState = "OUTDATED"
	skillModified   skillState = "MODIFIED"
	skillMissing    skillState = "MISSING"
	skillNotSkill   skillState = "NOT_SKILL"
	skillUnreadable skillState = "UNREADABLE"
)

// skillInstall is one location's row.
//
// Version is "" — not absent — for a skill with no stamp, and for a location with
// no skill at all: a script reading the JSON should not have to tell a missing key
// from an empty one to learn that there is nothing to compare.
type skillInstall struct {
	Scope   string     `json:"scope" yaml:"scope"`
	Dir     string     `json:"dir" yaml:"dir"`
	Version string     `json:"version" yaml:"version"`
	Status  skillState `json:"status" yaml:"status"`

	// Error is why an UNREADABLE location could not be looked at.
	Error string `json:"error,omitempty" yaml:"error,omitempty"`

	// fix is the command line that installs into this location — what the hint on
	// stderr tells the user to run.
	fix string
}

// skillStatusView is what `fft skill status` renders. FFT is the running binary's
// version, there so that a script can make the comparison itself.
type skillStatusView struct {
	FFT      string         `json:"fft" yaml:"fft"`
	Installs []skillInstall `json:"installs" yaml:"installs"`
}

// skillLocation is a directory the skill may be installed under.
//
// err is a location that could not even be worked out — no home directory, a
// working directory that has been deleted. It is carried rather than returned so
// that one location's trouble does not cost the report the other's row.
type skillLocation struct {
	scope string
	root  string
	fix   string
	err   error
}

func newSkillStatusCmd(deps *Deps) *cobra.Command {
	var (
		local bool
		dir   string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report which fft version each installed skill describes",
		Long:  skillStatusLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error {
			// The same reasoning as install's: a flag given and a directory not named is
			// not a request to look somewhere else.
			if cmd.Flags().Changed("dir") && dir == "" {
				return exitcode.UsageError{Err: errors.New("--dir needs a directory")}
			}
			return runSkillStatus(deps, local, dir)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&local, "local", false, "Look only in ./.claude/skills")
	f.StringVar(&dir, "dir", "", "Look only in this directory (the skill is DIR/fft)")

	cmd.MarkFlagsMutuallyExclusive("local", "dir")
	if err := cmd.MarkFlagDirname("dir"); err != nil {
		// A typo in the flag name above, and nothing else.
		panic(fmt.Sprintf("mark --dir as a directory: %v", err))
	}

	return cmd
}

func runSkillStatus(deps *Deps, local bool, dir string) error {
	locations := skillLocations(local, dir)

	view := skillStatusView{
		FFT:      buildinfo.Version,
		Installs: make([]skillInstall, 0, len(locations)),
	}
	for _, loc := range locations {
		view.Installs = append(view.Installs, inspectSkill(loc))
	}

	if err := deps.Printer.Render(skillStatusRows(deps, view), view); err != nil {
		return err
	}

	// Advice, not data — and only for a human, as with the update notice: under
	// -o json the status column already says everything these lines would.
	if deps.Printer.Format() == output.Table {
		for _, hint := range skillHints(view.Installs) {
			deps.Printer.Notef("%s", hint)
		}
	}
	return nil
}

// skillLocations are the directories to look in. With neither flag, that is both
// of the places Claude Code reads a skill from — the question is "is my agent
// reading a stale skill", and it reads whichever one it finds.
func skillLocations(local bool, dir string) []skillLocation {
	switch {
	case dir != "":
		return []skillLocation{{
			scope: "dir",
			root:  dir,
			fix:   "fft skill install --dir " + pastePath(dir),
		}}
	case local:
		return []skillLocation{projectLocation()}
	}
	return defaultSkillLocations()
}

// defaultSkillLocations are the personal and the project skill directories, each
// worked out on its own.
//
// When the two are one directory — run from the home directory, or with the
// project's .claude/skills, or its fft, a link to the personal one — it is
// reported once. One skill dressed up as two would be two rows, and two notices,
// about the same file.
func defaultSkillLocations() []skillLocation {
	user := skillLocation{scope: "user", fix: "fft skill install"}
	user.root, user.err = skill.UserDir()

	project := projectLocation()

	if user.err == nil && project.err == nil &&
		(sameDir(user.root, project.root) || sameDir(skillDir(user.root), skillDir(project.root))) {
		return []skillLocation{user}
	}
	return []skillLocation{user, project}
}

func projectLocation() skillLocation {
	loc := skillLocation{scope: "project", fix: "fft skill install --local"}
	loc.root, loc.err = skill.ProjectDir()
	return loc
}

// sameDir reports two paths that name one directory: spelled alike, or — through
// a symlink, or a filesystem that ignores case — the same file once followed.
func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// inspectSkill decides one location's state. It never fails: what goes wrong
// with one location is that location's row.
//
// The version decides first. A skill from another fft is OUTDATED whatever else
// is true of it, because the fix is the same either way — install — and install
// is what then asks about any edit it finds. MODIFIED is kept for the case the
// version cannot explain: a skill this fft wrote, that has since been changed.
func inspectSkill(loc skillLocation) skillInstall {
	row := skillInstall{Scope: loc.scope, fix: loc.fix}
	if loc.err != nil {
		row.Status, row.Error = skillUnreadable, loc.err.Error()
		return row
	}
	row.Dir = skillDir(loc.root)

	plan, err := skill.NewPlan(loc.root)
	switch {
	case errors.Is(err, skill.ErrNotSkill):
		row.Status = skillNotSkill
		return row
	case err != nil:
		row.Status, row.Error = skillUnreadable, err.Error()
		return row
	}
	row.Dir = plan.Dir

	meta, ok, err := skill.Installed(loc.root)
	switch {
	case errors.Is(err, skill.ErrMalformed):
		// A SKILL.md fft cannot read a version out of is not one fft wrote.
		row.Status = skillModified
	case err != nil:
		row.Status, row.Error = skillUnreadable, err.Error()
	case !ok:
		row.Status = skillMissing
	case !skill.Current(meta):
		row.Version = meta.Version()
		row.Status = skillOutdated
	case !untouched(plan):
		row.Version = meta.Version()
		row.Status = skillModified
	default:
		row.Version = meta.Version()
		row.Status = skillCurrent
	}
	return row
}

// untouched reports an install with nothing of the user's in it: every file
// either what this fft ships or what an older one wrote. The older ones only get
// here on a build that compares no versions — [skill.Current] has already ruled
// on everything else.
func untouched(plan skill.Plan) bool {
	for _, c := range plan.Files {
		switch c.Status {
		case skill.StatusUnchanged, skill.StatusOutdated, skill.StatusObsolete:
		default:
			return false
		}
	}
	return true
}

// skillDir is the skill's own directory under root, absolute where that can be
// worked out, so that what is printed is a path the user can act on.
func skillDir(root string) string {
	dir := filepath.Join(root, skill.Name)
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// pastePath spells a path for a command line fft prints for the user to paste,
// in the shell the platform's paths come from.
func pastePath(path string) string {
	return pastePathFor(path, runtime.GOOS == "windows")
}

// pastePathFor is [pastePath] for a named platform, so both spellings can be
// pinned on any machine.
//
// Elsewhere, single quotes, which a POSIX shell, fish and PowerShell all read
// the same way. On Windows the shell may be cmd.exe, which does not know single
// quotes at all, so a path that needs quoting is double-quoted with nothing
// escaped: cmd.exe and PowerShell both read a backslash inside double quotes as a
// backslash, and a Windows path cannot hold a double quote to escape.
func pastePathFor(path string, windows bool) string {
	if !windows {
		return tui.ShellQuote(path)
	}
	plain := !strings.ContainsFunc(path, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		default:
			return !strings.ContainsRune(`-_.:\/`, r)
		}
	})
	if plain {
		return path
	}
	return `"` + path + `"`
}

// staleSkill is the one sentence about a skill from another fft — the notice
// after any command, and the hint under `fft skill status`, say it the same way.
func staleSkill(dir, version, fix string) string {
	from := "an older fft"
	if version != "" {
		from = "fft " + version
	}
	return fmt.Sprintf("The fft skill in %s is from %s (you have %s) — run %s", dir, from, buildinfo.Version, fix)
}

// skillHints are the lines on stderr that say what to do about each location that
// is not current. A location with nothing in it is only worth a line when there
// is no skill anywhere: most people install one skill, and being told about the
// other location on every run would be telling them they did it wrong.
func skillHints(installs []skillInstall) []string {
	var hints []string
	missing := 0

	for _, in := range installs {
		switch in.Status {
		case skillOutdated:
			hints = append(hints, staleSkill(in.Dir, in.Version, in.fix))
		case skillModified:
			hints = append(hints, fmt.Sprintf("The fft skill in %s has been edited — %s --force puts back what fft ships",
				in.Dir, in.fix))
		case skillNotSkill:
			hints = append(hints, fmt.Sprintf("%s holds files that are not fft's skill, and fft will not install over them",
				in.Dir))
		case skillUnreadable:
			hints = append(hints, fmt.Sprintf("Cannot check the %s skill: %s", in.Scope, in.Error))
		case skillMissing:
			missing++
		}
	}

	if missing == len(installs) && missing > 0 {
		hints = append(hints, fmt.Sprintf("No fft skill is installed — run %s", installs[0].fix))
	}
	return hints
}

func skillStatusRows(deps *Deps, view skillStatusView) output.Rows {
	style := deps.Printer.Style()

	rows := make([][]string, 0, len(view.Installs))
	for _, in := range view.Installs {
		rows = append(rows, []string{in.Scope, cellOrDash(in.Dir), cellOrDash(in.Version), skillStateCell(style, in.Status)})
	}

	return output.Rows{
		Headers: []string{"SCOPE", "DIR", "VERSION", "STATUS"},
		Rows:    rows,
	}
}

func cellOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func skillStateCell(style output.Style, state skillState) string {
	switch state {
	case skillCurrent:
		return style.Green(string(state))
	case skillOutdated, skillModified:
		return style.Yellow(string(state))
	case skillNotSkill, skillUnreadable:
		return style.Red(string(state))
	default:
		return string(state)
	}
}
