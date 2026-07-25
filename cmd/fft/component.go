package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const componentLong = `Manage the components installed alongside fft.

A component is a building block somebody else wrote: a binary plus a manifest,
installed under ~/.local/share/fft/components, that adds commands to fft or
teaches the emulator to deliver events somewhere it does not know about. Once
installed, its commands appear in 'fft --help' like any other.

fft runs a component as you, with the environment it builds for it — so a
component only receives a tenant credential if its manifest asks for one, and
never receives the Firebase API key. That is a boundary, not a sandbox: a
component is code you chose to trust, in the way a shell alias or a git subcommand
is. Install ones you would run by hand.

Every install is pinned to a release and checked against that release's
checksums.txt before a byte of it is unpacked. Use --path to install a component
you built yourself.`

func newComponentCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "component",
		Aliases: []string{"components"},
		Short:   "Manage installed components",
		Long:    componentLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newComponentListCmd(deps),
		newComponentInfoCmd(deps),
		newComponentInstallCmd(deps),
		newComponentUpgradeCmd(deps),
		newComponentRemoveCmd(deps),
	)

	return cmd
}

// componentsDisabled reports FFT_COMPONENT_DIR set to the empty string, which is
// how a build, a spec or `fft gen-docs` pins a tree with no components in it.
//
// It is a usage error rather than a config one: nothing is broken, the caller asked
// for this, and the fix is in their own environment.
func componentsDisabled() error {
	return exitcode.UsageError{Err: fmt.Errorf(
		"components are disabled: %s is set to the empty string", component.EnvRoot)}
}

// componentView is one row of `fft component list`, and the document
// `fft component info` renders.
type componentView struct {
	Name        string `json:"name" yaml:"name"`
	Kind        string `json:"kind" yaml:"kind"`
	Version     string `json:"version,omitempty" yaml:"version,omitempty"`
	Status      string `json:"status" yaml:"status"`
	Source      string `json:"source,omitempty" yaml:"source,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Origin is "first-party" for a component fft ships and "community" for anything
	// else. It is the one thing a user cannot work out from the rest of the row, and
	// it is what decides how much the other columns are worth trusting.
	Origin string `json:"origin" yaml:"origin"`

	// Commands are the commands it adds, or the target types it delivers.
	Commands []string `json:"commands,omitempty" yaml:"commands,omitempty"`
	Targets  []string `json:"targets,omitempty" yaml:"targets,omitempty"`
}

// The statuses a component can be in.
const (
	// statusInstalled: the manifest and the binary are both there.
	statusInstalled = "installed"

	// statusAvailable: fft knows about it and it is not installed. Only a first-party
	// component can be in this state, which is what lets `fft emulator` explain
	// itself rather than not existing.
	statusAvailable = "available"
)

func newComponentView(c component.Component) componentView {
	v := componentView{
		Name:        c.Name,
		Kind:        string(c.Kind),
		Version:     c.Version,
		Status:      statusAvailable,
		Source:      c.Source,
		Description: c.Description,
		Origin:      "community",
		Targets:     c.Targets,
	}
	if c.Installed {
		v.Status = statusInstalled
	}
	if c.FirstParty {
		v.Origin = "first-party"
		if v.Source == "" {
			v.Source = component.DefaultRepo
		}
	}
	for _, cmd := range c.Commands {
		v.Commands = append(v.Commands, "fft "+cmd.Name)
	}
	return v
}

var componentHeaders = []string{"NAME", "KIND", "VERSION", "STATUS", "ORIGIN", "SOURCE"}

func componentRows(style output.Style, views []componentView) output.Rows {
	rows := make([][]string, 0, len(views))
	for _, v := range views {
		rows = append(rows, []string{
			field(style, v.Name),
			field(style, v.Kind),
			field(style, v.Version),
			componentStatusCell(style, v.Status),
			field(style, v.Origin),
			field(style, v.Source),
		})
	}
	return output.Rows{Headers: componentHeaders, Rows: rows}
}

// componentStatusCell colours the column a reader scans for: a component that is
// not installed is one whose commands will not run.
func componentStatusCell(style output.Style, status string) string {
	if status == statusInstalled {
		return style.Green(status)
	}
	return style.Yellow(status)
}

// reportProblems warns about directories under the component root that are not
// usable components.
//
// On stderr, and never fatal. One component with a manifest fft cannot read must
// not stop the others from working — but it must not be silently omitted either,
// because "I installed it and it is not in the list" is a much worse afternoon than
// "I installed it and fft says why it is ignoring it".
func reportProblems(deps *Deps, reg *component.Registry) {
	for _, p := range reg.Problems() {
		deps.Printer.Warnf("ignoring %s: %v", p.Dir, p.Err)
	}
}

// componentNames completes an installed component's name.
func completeComponentNames(deps *Deps) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
		var names []string
		for _, c := range deps.Components.All() {
			if strings.HasPrefix(c.Name, prefix) {
				names = append(names, c.Name)
			}
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}
