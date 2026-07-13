package main

import (
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/config"
)

const projectCurrentLong = `Show the project fft would act on.

That is: --project or FFT_PROJECT if either is set, otherwise the project
synthesized from FFT_BASE_URL and friends, otherwise the active project from the
config file. If none of those yields a project, this exits 3.`

func newProjectCurrentCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "current",
		Short:   "Show the active project",
		Long:    projectCurrentLong,
		Aliases: []string{"show"},
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			return runProjectCurrent(deps)
		},
	}
}

func runProjectCurrent(deps *Deps) error {
	project, err := deps.ActiveProject()
	if err != nil {
		return err
	}

	view := newProjectView(project, true, deps.Secrets)
	if err := deps.Printer.Render(projectRows([]projectView{view}), view); err != nil {
		return err
	}

	// A session made read-only by FFT_READ_ONLY or --read-only appears nowhere in the
	// table — the project itself is writable, and the table describes the project. So
	// it is said out loud, on stderr: otherwise the user reads "writable", watches the
	// next write get refused, and has nothing to connect the two.
	if source, blocked := deps.readOnlySource(project); blocked && !view.ReadOnly {
		switch source {
		case sourceEnv:
			deps.Printer.Notef("This session is read-only: %s is set, so writes will be refused.", config.EnvReadOnly)
		case sourceFlag:
			deps.Printer.Notef("This session is read-only: --read-only was given, so writes will be refused.")
		}
	}
	return nil
}
