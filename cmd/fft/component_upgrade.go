package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const componentUpgradeLong = `Reinstall a component from the latest release of wherever it came from.

The source recorded at install time is where fft looks; a component installed
with --path has no release to upgrade from and is refused rather than silently
reinstalled from somewhere else.

An upgrade is an install with the same confirmation: you see the new version and
what it asks for before anything is replaced. A failed upgrade leaves the working
version in place.`

func newComponentUpgradeCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade <name>",
		Short: "Upgrade an installed component",
		Long:  componentUpgradeLong,
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := upgradeSource(deps, args[0])
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			return runComponentInstall(ctx, deps, src, "Upgrade")
		},
	}

	cmd.ValidArgsFunction = completeComponentNames(deps)
	return cmd
}

// upgradeSource works out where an installed component should be re-fetched from.
func upgradeSource(deps *Deps, name string) (component.Source, error) {
	c, ok := deps.Components.Lookup(name)
	if !ok {
		return component.Source{}, exitcode.UsageError{Err: fmt.Errorf(
			"no component called %q; run 'fft component list' to see what is installed", name)}
	}

	// A first-party component that has never been installed has no recorded source,
	// and does not need one: it comes from fft's own releases by definition.
	if c.Source == "" {
		if c.FirstParty {
			return component.Source{Repo: component.DefaultRepo, Name: c.Name}, nil
		}
		return component.Source{}, exitcode.UsageError{Err: fmt.Errorf(
			"%s does not record where it came from, so there is nothing to upgrade from", name)}
	}

	// Only a GitHub source can be fetched again. A --path install records the
	// directory it was copied from — and a bare relative one like `mine` would parse
	// as a component name and resolve, wrongly, to fft's own repo. So the prefix is
	// required rather than inferred: anything else is a local install with no release
	// behind it, and upgrading it is refused as documented.
	repo, ok := githubRepo(c.Source)
	if !ok {
		return component.Source{}, exitcode.UsageError{Err: fmt.Errorf(
			"%s was installed from %q, which is not a GitHub release fft can fetch again", name, c.Source)}
	}

	src, err := component.ParseSource(repo)
	if err != nil {
		return component.Source{}, exitcode.UsageError{Err: fmt.Errorf(
			"%s was installed from %q, which is not a release fft can fetch again: %w", name, c.Source, err)}
	}

	// The recorded source pins the version that *was* installed. An upgrade wants the
	// latest, so the pin is dropped and the name kept.
	src.Version = ""
	src.Name = c.Name
	return src, nil
}

// githubRepo turns a recorded source into the owner/repo ParseSource understands, and
// reports whether it was a GitHub source at all. Install records
// "github.com/owner/repo@v1"; a --path install records a directory, which has no such
// prefix and is not fetchable.
func githubRepo(source string) (string, bool) {
	spec, _, _ := strings.Cut(source, "@")
	rest, ok := strings.CutPrefix(spec, "github.com/")
	return rest, ok
}
