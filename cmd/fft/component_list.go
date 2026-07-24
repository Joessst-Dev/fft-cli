package main

import (
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/component"
)

const componentListLong = `List the components fft can see.

STATUS is "installed" when the binary is there and "available" when fft knows
about the component but it has not been installed — which only ever applies to a
component fft ships itself, since those are registered whether or not they are
present. ORIGIN says whether fft ships it or somebody else does.

A directory under the component root that fft cannot read as a component is
reported on stderr and left out of the list.`

func newComponentListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List installed components",
		Long:    componentListLong,
		Aliases: []string{"ls"},
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			return runComponentList(deps)
		},
	}
}

func runComponentList(deps *Deps) error {
	reportProblems(deps, deps.Components)

	// Said whether or not anything is listed, because the list on its own does not
	// show it: a first-party component is registered even with components switched
	// off, so the table looks ordinary while every install and remove would refuse.
	if deps.Components.Root() == "" {
		deps.Printer.Notef("Components are disabled: %s is set to the empty string.", component.EnvRoot)
	}

	all := deps.Components.All()
	if len(all) == 0 {
		// Not an error: it is what a build with no first-party components and nothing
		// installed looks like. Empty says there are none — on stderr for a table, as
		// `[]` on stdout for JSON.
		return deps.Printer.Empty("components")
	}

	views := make([]componentView, 0, len(all))
	for _, c := range all {
		views = append(views, newComponentView(c))
	}
	return deps.Printer.Render(componentRows(deps.Printer.Style(), views), views)
}
