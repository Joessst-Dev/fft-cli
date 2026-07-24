package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const componentInfoLong = `Show what a component is and what it adds.

Under -o json or -o yaml this prints the component's manifest as fft read it:
which commands it registers, which session each of them receives, whether it
declares that it changes the tenant, and which environment variables it consumes.

That is the whole of what fft knows about a component before running it, so it is
also the whole of what there is to review.`

func newComponentInfoCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "Show a component's manifest",
		Long:  componentInfoLong,
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			return runComponentInfo(deps, args[0])
		},
	}

	cmd.ValidArgsFunction = completeComponentNames(deps)
	return cmd
}

func runComponentInfo(deps *Deps, name string) error {
	c, ok := deps.Components.Lookup(name)
	if !ok {
		return exitcode.UsageError{Err: fmt.Errorf(
			"no component called %q; run 'fft component list' to see what is installed", name)}
	}

	view := newComponentView(c)
	rows := output.Rows{
		Headers: []string{"FIELD", "VALUE"},
		Rows: [][]string{
			{"name", field(deps.Printer.Style(), view.Name)},
			{"kind", field(deps.Printer.Style(), view.Kind)},
			{"version", field(deps.Printer.Style(), view.Version)},
			{"status", componentStatusCell(deps.Printer.Style(), view.Status)},
			{"origin", field(deps.Printer.Style(), view.Origin)},
			{"source", field(deps.Printer.Style(), view.Source)},
			{"path", field(deps.Printer.Style(), c.ExecPath())},
		},
	}
	for _, cmd := range c.Commands {
		rows.Rows = append(rows.Rows, []string{
			"command",
			fmt.Sprintf("fft %s (session %s, mutates %t)", cmd.Name, cmd.Session, cmd.Mutates),
		})
	}
	for _, target := range c.Targets {
		rows.Rows = append(rows.Rows, []string{"target", field(deps.Printer.Style(), target)})
	}
	for _, name := range c.Env {
		rows.Rows = append(rows.Rows, []string{"env", field(deps.Printer.Style(), name)})
	}

	// Render, not RenderRaw: the manifest is fft's document, read and validated, not
	// bytes some API sent. -o json is what fft understood it to say, which is the
	// thing worth reviewing.
	return deps.Printer.Render(rows, c.Manifest)
}
