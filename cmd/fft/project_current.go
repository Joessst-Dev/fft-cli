package main

import (
	"github.com/spf13/cobra"
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
	return deps.Printer.Render(projectRows([]projectView{view}), view)
}
