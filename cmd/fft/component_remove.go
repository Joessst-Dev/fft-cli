package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const componentRemoveLong = `Remove an installed component.

The component's directory is deleted, and its commands stop appearing in
'fft --help'. A component fft ships stays listed as "available", because fft
knows about it whether or not it is installed.

fft refuses a directory that holds no component manifest, whatever the name says:
the name comes from a shell, where a typo is one keystroke, and fft will not
recursively delete a directory it cannot prove it created.`

func newComponentRemoveCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <name>",
		Short:   "Remove an installed component",
		Long:    componentRemoveLong,
		Aliases: []string{"rm", "uninstall"},
		Args:    usageArgs(cobra.ExactArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			return runComponentRemove(deps, args[0])
		},
	}

	cmd.ValidArgsFunction = completeComponentNames(deps)
	return cmd
}

func runComponentRemove(deps *Deps, name string) error {
	root := deps.Components.Root()
	if root == "" {
		return componentsDisabled()
	}

	c, ok := deps.Components.Lookup(name)
	if !ok || !c.Installed {
		return exitcode.UsageError{Err: fmt.Errorf("%s is not installed", name)}
	}

	ok, err := confirmDestructive(deps, fmt.Sprintf("Remove the %s component from %s?", name, c.Dir))
	if err != nil {
		return err
	}
	if !ok {
		deps.Printer.Notef("Aborted; %s was not removed.", name)
		return nil
	}

	if err := deps.NewInstaller(root).Remove(name); err != nil {
		return err
	}

	deps.Printer.Notef("Removed %s.", name)
	return nil
}
