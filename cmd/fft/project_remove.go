package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

const projectRemoveLong = `Remove a project.

Its entry in the config file goes, and so does every one of its stored secrets —
the password, the refresh token, the cached id token and its expiry. Nothing is
left behind for a later project of the same name to inherit.

Both stores are emptied, not just the one in use: a machine that switched to the
0600 file (--no-keyring, settings.noKeyring) left whatever was already in its
keychain behind, and this is where that gets cleared up.

fft only claims to have removed the credentials when it actually did. If the
other store refuses — a locked keychain, an unreadable file — or cannot be opened
at all, as over SSH with no session bus, it says which store and where, because
what is left there is beyond fft's reach and not beyond yours.

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

	// And the store this machine is not using. settings.noKeyring changes where fft
	// looks without moving what was already stored, so a project configured before
	// the switch still has its password in the keychain — and this is the command
	// that promises to leave nothing behind.
	other := deps.unusedSecrets()
	outcome := swept
	if other != nil {
		outcome = sweep(deps, other, project.Name)
	}

	// An unreachable store is not an empty one, and this is the command where the
	// difference matters. A laptop whose credentials are in gnome-keyring, driven
	// over SSH with no session bus, would otherwise be told its password had been
	// destroyed while it sat there in a keychain the next desktop login can read.
	// `project add` treats the same silence as "nothing to do" because there it
	// means the write just failed on this machine; here it means fft cannot look.
	if outcome == unreachable {
		deps.Printer.Warnf(
			"could not open %s, so anything %q had there is still there.",
			storeLocation(other), project.Name)
	}

	cfg.Remove(project.Name)
	if err := deps.SaveConfig(cfg); err != nil {
		return err
	}

	// Only claim the credentials when they are actually gone. Both other outcomes
	// have already said where the rest are, and why.
	if outcome == swept {
		deps.Printer.Notef("Removed project %q and its stored credentials.", project.Name)
	} else {
		deps.Printer.Notef("Removed project %q.", project.Name)
	}
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
