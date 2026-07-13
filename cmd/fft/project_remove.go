package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

const projectRemoveLong = `Remove a project.

Its entry in the config file goes, and so does every one of its keychain entries
— the password, the refresh token, the cached id token and its expiry. Nothing is
left behind for a later project of the same name to inherit.

Removing the active project leaves no project active; run 'fft project use' to
pick another.`

func newProjectRemoveCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:               "remove <name>",
		Short:             "Remove a project and its stored credentials",
		Long:              projectRemoveLong,
		Aliases:           []string{"rm", "delete"},
		Args:              usageArgs(cobra.ExactArgs(1)),
		ValidArgsFunction: completeProjectNames(deps),
		RunE: func(_ *cobra.Command, args []string) error {
			return runProjectRemove(deps, args[0])
		},
	}
}

func runProjectRemove(deps *Deps, name string) error {
	if err := deps.requireMutableConfig("remove"); err != nil {
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

	confirmed, err := confirmRemoval(deps, project.Name)
	if err != nil {
		return err
	}
	if !confirmed {
		deps.Printer.Notef("Aborted; %q was not removed.", project.Name)
		return nil
	}

	// The keychain is emptied first. If it were done second and the config write
	// succeeded but the keychain delete failed, the user would have no project to
	// name in a retry and the secrets would be stranded — invisible to fft and
	// still sitting in their keychain.
	if err := secrets.DeleteAll(deps.Secrets, project.Name); err != nil {
		return fmt.Errorf("remove the stored credentials for %q: %w", project.Name, err)
	}

	cfg.Remove(project.Name)
	if err := deps.SaveConfig(cfg); err != nil {
		return err
	}

	deps.Printer.Notef("Removed project %q and its stored credentials.", project.Name)
	if cfg.ActiveProject == "" && len(cfg.Projects) > 0 {
		deps.Printer.Notef("There is no active project now. Run 'fft project use <name>' to pick one.")
	}
	return nil
}

// confirmRemoval asks before destroying credentials — unless -y was given, or
// there is no terminal to ask on, in which case it refuses rather than assuming.
//
// Assuming yes on a non-TTY is how a script that forgot -y quietly deletes
// something. Refusing is noisy, and noisy is the right failure mode here.
func confirmRemoval(deps *Deps, name string) (bool, error) {
	if deps.AssumeYes {
		return true, nil
	}

	if !deps.Prompt.Interactive() {
		return false, exitcode.UsageError{Err: errors.New(
			"stdin is not a terminal, so fft cannot ask for confirmation: pass --yes to remove the project")}
	}

	return deps.Prompt.Confirm(fmt.Sprintf("Remove project %q and its stored credentials?", name))
}
