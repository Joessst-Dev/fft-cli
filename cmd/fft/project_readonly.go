package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const projectReadOnlyLong = `Mark a project read-only, or allow writes to it again.

A read-only project refuses every request that would change the tenant — creates,
updates, patches, deletes and the action endpoints — before it signs in, so a
refused write costs no round trip and mints no token. It exits 10.

Reads keep working, and that includes the searches behind every list command: the
API runs those over POST, so being read-only is not the same as refusing to POST.

Marking a project read-only does not lock the config file. 'fft project use' and
'fft project remove' still work on it, because configuring fft is not changing
anything in the tenant.

Two other ways to say the same thing: FFT_READ_ONLY=1 protects whichever project
fft is about to use, which is what a CI job wants, and a --read-only flag on any
command protects that one invocation. Neither can be talked back down — the flag
and the variable can only tighten, never loosen.`

func newProjectReadOnlyCmd(deps *Deps) *cobra.Command {
	var off bool

	cmd := &cobra.Command{
		// The name is positional and required, like `use` and `remove` and unlike
		// `current`: allowing it to default to the active project is exactly the
		// accident this command exists to prevent — `fft project read-only --off` with
		// prod merely *active* would disarm the wrong tenant.
		Use:               "read-only <name>",
		Short:             "Refuse every request that would change a project",
		Long:              projectReadOnlyLong,
		Aliases:           []string{"readonly", "ro"},
		Args:              usageArgs(cobra.ExactArgs(1)),
		ValidArgsFunction: completeProjectNames(deps),
		RunE: func(_ *cobra.Command, args []string) error {
			return runProjectReadOnly(deps, args[0], off)
		},
	}

	// The safe direction is the default, and the dangerous one has to be spelled out.
	cmd.Flags().BoolVar(&off, "off", false, "Allow writes to the project again")

	return cmd
}

func runProjectReadOnly(deps *Deps, name string, off bool) error {
	// Headless mode owns this answer through FFT_READ_ONLY, and writing a config
	// file the next command would ignore is precisely what requireMutableConfig is
	// there to refuse.
	if err := deps.requireMutableConfig("change"); err != nil {
		return err
	}

	cfg, err := deps.LoadConfig()
	if err != nil {
		return err
	}

	project, err := cfg.Resolve(name)
	if err != nil {
		return err
	}

	readOnly := !off

	// Idempotent, and silently so: a provisioning script that runs this on every
	// deploy should not have to care whether it ran yesterday, and re-writing the
	// config file to the value it already holds is a change to nothing.
	if project.ReadOnly == readOnly {
		deps.Printer.Notef("Project %q is already %s.", project.Name, access(readOnly))
		return renderProject(deps, cfg, project)
	}

	if off {
		confirmed, err := confirmWritable(deps, project.Name)
		if err != nil {
			return err
		}
		if !confirmed {
			deps.Printer.Notef("Aborted; %q is still read-only.", project.Name)
			return nil
		}
	}

	project.ReadOnly = readOnly
	cfg.Upsert(project)
	if err := deps.SaveConfig(cfg); err != nil {
		return err
	}

	if readOnly {
		deps.Printer.Notef("Project %q is read-only: fft will refuse every request that would change it.", project.Name)
	} else {
		deps.Printer.Warnf("Project %q accepts writes again.", project.Name)
	}
	return renderProject(deps, cfg, project)
}

// renderProject prints the project's new state, so that `-o json` gets an answer on
// stdout rather than only a sentence on stderr.
func renderProject(deps *Deps, cfg *config.Config, project config.Project) error {
	view := newProjectView(project, cfg.ActiveProject == project.Name, deps.Secrets)
	return deps.Printer.Render(projectRows([]projectView{view}), view)
}

func access(readOnly bool) string {
	if readOnly {
		return "read-only"
	}
	return "writable"
}

// confirmWritable asks before re-arming writes — unless -y was given, or there is
// no terminal to ask on, in which case it refuses rather than assuming.
//
// This is the one direction of this command that can lose data: it is the moment a
// protected production tenant stops being protected. A script that forgot --yes
// should be noisy about it, exactly as `project remove` is.
func confirmWritable(deps *Deps, name string) (bool, error) {
	if deps.AssumeYes {
		return true, nil
	}

	if !deps.Prompt.Interactive() {
		return false, exitcode.UsageError{Err: errors.New(
			"stdin is not a terminal, so fft cannot ask for confirmation: pass --yes to allow writes again")}
	}

	return deps.Prompt.Confirm(fmt.Sprintf("Allow writes to project %q again?", name))
}
