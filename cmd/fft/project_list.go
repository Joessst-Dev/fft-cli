package main

import (
	"github.com/spf13/cobra"
)

const projectListLong = `List the configured projects.

The active project is marked with an asterisk. CREDENTIAL says where the
project's password or token is: "keyring" for the OS keychain, "file" for the
--no-keyring fallback, "env" for a project synthesized from FFT_* variables, and
"missing" for a project whose secrets have been removed from the keychain behind
fft's back — it is configured, but nothing can sign in as it.

In headless mode (FFT_BASE_URL and friends) this lists the environment's project
and does not read the config file.`

func newProjectListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List the configured projects",
		Long:    projectListLong,
		Aliases: []string{"ls"},
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			return runProjectList(deps)
		},
	}
}

func runProjectList(deps *Deps) error {
	views, err := listProjectViews(deps)
	if err != nil {
		return err
	}

	// An empty list is not an error: it is a first run. Say so on stderr, and
	// leave stdout empty so that `-o json` still yields a parseable `[]`.
	if len(views) == 0 {
		deps.Printer.Notef("No projects are configured. Run 'fft project add <name>'.")
	}
	return deps.Printer.Render(projectRows(views), views)
}

func listProjectViews(deps *Deps) ([]projectView, error) {
	// A headless run reports only the environment's project. Reading the config
	// file here would list projects that no command in this process could
	// actually select, since the environment wins.
	if deps.Ephemeral != nil {
		return []projectView{newProjectView(*deps.Ephemeral, true, deps.Secrets)}, nil
	}

	cfg, err := deps.LoadConfig()
	if err != nil {
		return nil, err
	}

	views := make([]projectView, 0, len(cfg.Projects))
	for _, p := range cfg.Projects {
		views = append(views, newProjectView(p, p.Name == cfg.ActiveProject, deps.Secrets))
	}
	return views, nil
}
