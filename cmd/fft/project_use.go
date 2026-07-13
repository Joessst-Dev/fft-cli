package main

import (
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

const projectUseLong = `Make a project the active one.

Every command then acts on it until you switch again. --project still overrides
the choice for a single command.`

func newProjectUseCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:               "use <name>",
		Short:             "Set the active project",
		Long:              projectUseLong,
		Args:              usageArgs(cobra.ExactArgs(1)),
		ValidArgsFunction: completeProjectNames(deps),
		RunE: func(_ *cobra.Command, args []string) error {
			return runProjectUse(deps, args[0])
		},
	}
}

func runProjectUse(deps *Deps, name string) error {
	if err := deps.requireMutableConfig("switch to"); err != nil {
		return err
	}

	cfg, err := deps.LoadConfig()
	if err != nil {
		return err
	}

	// Resolve rather than Find, so that an unknown name produces the same
	// "here are the projects you do have" error as every other command.
	project, err := cfg.Resolve(name)
	if err != nil {
		return err
	}

	cfg.ActiveProject = project.Name
	if err := deps.SaveConfig(cfg); err != nil {
		return err
	}

	// A project whose secrets have gone missing is still selectable — you may be
	// about to re-add them — but the user should hear about it now rather than on
	// the next command's authentication failure.
	if !secrets.Has(deps.Secrets, project.Name) {
		deps.Printer.Warnf("no credentials are stored for %q. Run 'fft project add %s --force' to set them.",
			project.Name, project.Name)
	}

	deps.Printer.Notef("Now using project %q.", project.Name)
	return nil
}

// completeProjectNames suggests the configured project names for shell
// completion. Completion must never fail loudly — a broken config file should
// produce no suggestions, not an error message halfway through the user's line.
func completeProjectNames(deps *Deps) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		cfg, err := deps.LoadConfig()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		names := make([]string, 0, len(cfg.Projects))
		for _, p := range cfg.Projects {
			names = append(names, p.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}
