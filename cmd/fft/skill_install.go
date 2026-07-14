package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/skill"
)

const skillInstallLong = `Copy the agent skill into a directory an AI assistant reads.

By default that is ~/.claude/skills/fft, where Claude Code finds a personal skill —
one installed once, available in every project. --local installs it into
./.claude/skills/fft instead, where it belongs to this project alone and can be
committed with it. --dir puts it wherever you say, for an agent that reads skills
from somewhere else.

Installing twice is silent and changes nothing: fft compares what it ships against
what is there. A file you have edited is a CONFLICT, and fft asks before replacing
it — or refuses, with exit 2, if there is no terminal to ask on. --force answers
that question in advance.

The skill needs no project, no credentials and no network, so this is a reasonable
first thing to run on a new machine.`

// skillView is what `fft skill install` renders.
//
// The directory is on stdout, not merely in a sentence on stderr: where the skill
// landed is the answer to the command, and a path only a human can read is not an
// answer a script can use.
type skillView struct {
	Skill string         `json:"skill" yaml:"skill"`
	Dir   string         `json:"dir" yaml:"dir"`
	Files []skill.Change `json:"files" yaml:"files"`
}

func newSkillInstallCmd(deps *Deps) *cobra.Command {
	var (
		local bool
		dir   string
		force bool
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the skill for an AI assistant to read",
		Long:  skillInstallLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// --dir "" is a flag given and a directory not named. Falling through to the
			// home directory would install the skill somewhere the user did not ask for
			// and did not say, which is the sort of quiet substitution that gets found
			// out much later.
			if cmd.Flags().Changed("dir") && dir == "" {
				return exitcode.UsageError{Err: errors.New("--dir needs a directory")}
			}
			return runSkillInstall(deps, local, dir, force)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&local, "local", false, "Install into ./.claude/skills instead of ~/.claude/skills")
	f.StringVar(&dir, "dir", "", "Install into this directory instead (the skill lands in DIR/fft)")
	f.BoolVar(&force, "force", false, "Replace files that differ from the ones fft ships")

	cmd.MarkFlagsMutuallyExclusive("local", "dir")
	if err := cmd.MarkFlagDirname("dir"); err != nil {
		// A typo in the flag name above, and nothing else.
		panic(fmt.Sprintf("mark --dir as a directory: %v", err))
	}

	return cmd
}

func runSkillInstall(deps *Deps, local bool, dir string, force bool) error {
	root, err := skillRoot(local, dir)
	if err != nil {
		return err
	}

	plan, err := skill.NewPlan(root)
	if err != nil {
		if errors.Is(err, skill.ErrNotSkill) {
			// The user named a directory that is not fft's, so this is a problem with the
			// command line and not with the machine — and fft will not start deleting
			// files out of a directory it does not recognise, --force or no --force.
			return exitcode.UsageError{Err: fmt.Errorf(
				"%w: pass --dir to choose another directory", err)}
		}
		return err
	}

	if pending := plan.Pending(); len(pending) > 0 {
		confirmed, err := confirmSkillOverwrite(deps, plan.Dir, pending, force)
		if err != nil {
			return err
		}
		if !confirmed {
			deps.Printer.Notef("Aborted; nothing was written.")
			return nil
		}
	}

	done, err := plan.Apply()
	if err != nil {
		return err
	}

	if unchanged(done) {
		deps.Printer.Notef("The %s skill in %s is already up to date.", skill.Name, done.Dir)
	} else {
		deps.Printer.Notef("Installed the %s skill to %s.", skill.Name, done.Dir)
		if local {
			deps.Printer.Notef("Claude Code picks up a project skill the next time it runs in this directory.")
		}
	}

	return deps.Printer.Render(skillRows(deps, done), skillView{
		Skill: skill.Name,
		Dir:   done.Dir,
		Files: done.Files,
	})
}

// skillRoot is the directory the skill goes under — not the skill's own directory,
// which is always <root>/fft.
func skillRoot(local bool, dir string) (string, error) {
	switch {
	case dir != "":
		return dir, nil
	case local:
		root, err := skill.ProjectDir()
		if err != nil {
			return "", config.NewError(err, "Pass --dir to say where the skill should go.")
		}
		return root, nil
	default:
		root, err := skill.UserDir()
		if err != nil {
			// The same class of failure as an unresolvable config directory, and the same
			// exit code: fft cannot work out where a file of yours belongs.
			return "", config.NewError(err, "Pass --dir to say where the skill should go.")
		}
		return root, nil
	}
}

// confirmSkillOverwrite asks before replacing a file the user has edited, or
// removing one fft no longer ships.
//
// It names --force rather than --yes when there is no terminal, though either
// works: --force is the flag documented for this, and sending someone to the other
// one would be a small lie in a message whose only job is to be followed. An
// agent's shell is not a terminal, which means an agent that would overwrite a
// human's edited skill stops here — which is the point.
func confirmSkillOverwrite(deps *Deps, dir string, pending []skill.Change, force bool) (bool, error) {
	if force || deps.AssumeYes {
		return true, nil
	}

	for _, c := range pending {
		deps.Printer.Warnf("%s: %s", c.Status, c.File)
	}

	if !deps.Prompt.Interactive() {
		return false, exitcode.UsageError{Err: fmt.Errorf(
			"%s in %s %s not what fft ships, and stdin is not a terminal, so fft cannot ask: pass --force to replace them",
			count(len(pending), "file"), dir, plural(len(pending), "is", "are"))}
	}

	return deps.Prompt.Confirm(fmt.Sprintf("Replace them in %s?", dir))
}

// unchanged reports an install that had nothing to do — which is the usual case
// for the second and every later run, and is worth saying out loud rather than
// claiming to have installed something.
func unchanged(plan skill.Plan) bool {
	for _, c := range plan.Files {
		if c.Status != skill.StatusUnchanged {
			return false
		}
	}
	return true
}

// count is "1 file" or "3 files". [plural], in stock_create.go, picks the word.
func count(n int, noun string) string {
	return fmt.Sprintf("%d %s", n, plural(n, noun, noun+"s"))
}

func skillRows(deps *Deps, plan skill.Plan) output.Rows {
	style := deps.Printer.Style()

	rows := make([][]string, 0, len(plan.Files))
	for _, c := range plan.Files {
		rows = append(rows, []string{c.File, skillStatusCell(style, c.Status)})
	}

	return output.Rows{
		Headers: []string{"FILE", "STATUS"},
		Rows:    rows,
	}
}

func skillStatusCell(style output.Style, status skill.Status) string {
	switch status {
	case skill.StatusWritten:
		return style.Green(string(status))
	case skill.StatusReplaced, skill.StatusRemoved:
		return style.Yellow(string(status))
	default:
		return string(status)
	}
}
