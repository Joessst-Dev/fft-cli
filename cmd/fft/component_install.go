package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const componentInstallLong = `Install a component.

Three ways to name one:

  fft component install emulator            a component fft ships
  fft component install owner/repo          somebody else's, latest release
  fft component install owner/repo@v1.2.0   somebody else's, pinned
  fft component install --path ./mine       one you built yourself

The archive is downloaded, checked against the release's checksums.txt, and
unpacked into a staging directory. Nothing is installed until you have seen what
it is and said yes; -y/--yes answers for you.

A component runs as you, with whatever access you have. fft decides which
credentials it receives — a component only gets a tenant token if its manifest
asks for one, and never gets the Firebase API key — but it cannot stop code you
installed from doing what code can do. Read 'fft component info' before you say
yes to something you did not write.`

func newComponentInstallCmd(deps *Deps) *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "install [<name>|<owner>/<repo>[@<version>]]",
		Short: "Install a component",
		Long:  componentInstallLong,
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := installSource(args, path)
			if err != nil {
				return err
			}

			// The command's own context, not deps.Context: a component download is not a
			// single API call, and the default 30s --timeout would cut off a large binary
			// on a slow link — the very thing the installer's own client documents itself
			// as leaving to the caller. Ctrl-C still cancels; the installer caps the body
			// size so an unbounded context is not an unbounded read.
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			return runComponentInstall(ctx, deps, src, "Install")
		},
	}

	cmd.Flags().StringVar(&path, "path", "",
		"Install from a local, unpacked component directory instead of a release")
	return cmd
}

// installSource turns the arguments into a source, refusing the two command lines
// that contradict themselves.
func installSource(args []string, path string) (component.Source, error) {
	switch {
	case path != "" && len(args) > 0:
		return component.Source{}, exitcode.UsageError{Err: fmt.Errorf(
			"--path installs from a directory, so there is nothing for %q to name", args[0])}
	case path != "":
		return component.Source{Dir: path}, nil
	case len(args) == 0:
		return component.Source{}, exitcode.UsageError{Err: fmt.Errorf(
			"name a component to install, or pass --path")}
	}

	src, err := component.ParseSource(args[0])
	if err != nil {
		return component.Source{}, exitcode.UsageError{Err: err}
	}
	return src, nil
}

// runComponentInstall stages, asks, and commits. verb is the word the question and
// the summary use, so that an upgrade reads as one.
func runComponentInstall(ctx context.Context, deps *Deps, src component.Source, verb string) error {
	root := deps.Components.Root()
	if root == "" {
		return componentsDisabled()
	}

	installer := deps.NewInstaller(root)

	deps.Printer.Notef("Fetching %s…", src)
	plan, err := installer.Prepare(ctx, src)
	if err != nil {
		return err
	}

	// From here every path must either commit or discard: a staging directory left
	// behind is litter in the user's home that nothing will ever clean up.
	ok, err := confirmInstall(deps, plan, verb)
	if err != nil || !ok {
		installer.Discard(plan)
		if err != nil {
			return err
		}
		deps.Printer.Notef("Aborted; %s was not installed.", plan.Manifest.Name)
		return nil
	}

	installed, err := installer.Commit(plan)
	if err != nil {
		return err
	}

	// stderr, and nothing on stdout: an install has no data to emit, and a cheerful
	// sentence in the pipe of whoever scripted it is exactly what the output contract
	// is there to prevent.
	named := installed.Name
	if installed.Version != "" {
		named += " " + installed.Version
	}
	deps.Printer.Notef("Installed %s into %s.", named, installed.Dir)
	for _, cmd := range installed.Commands {
		deps.Printer.Notef("  fft %s — %s", cmd.Name, cmd.Short)
	}
	for _, target := range installed.Targets {
		deps.Printer.Notef("  delivers %s for the emulator", target)
	}
	return nil
}

// confirmInstall shows what was fetched and asks whether to install it.
//
// The question is the only point at which a human decides to trust this code, so it
// says the three things that decide it: where it came from, whether the download
// matched what the release says it is, and what it will be allowed to do.
func confirmInstall(deps *Deps, plan component.Plan, verb string) (bool, error) {
	// Every line of this goes to stderr, like every other notice. The summary is for
	// the human being asked; a script that pipes this command is reading nothing.
	notef := deps.Printer.Notef

	notef("")
	notef("  %s %s", plan.Manifest.Name, plan.Manifest.Version)
	if plan.Manifest.Description != "" {
		notef("  %s", plan.Manifest.Description)
	}
	notef("  from     %s", plan.Source)
	notef("  verified %s", plan.Verification())
	if plan.Replaces != "" {
		notef("  replaces %s", plan.Replaces)
	}
	notef("  into     %s", plan.Dir)

	printGrants(notef, plan.Manifest)
	notef("")
	notef("A component runs as you. Install one you would run by hand.")

	return confirmDestructive(deps, fmt.Sprintf("%s %s?", verb, plan.Manifest.Name))
}

// printGrants spells out what the manifest is asking for, per command, in the terms
// the read-only gate will actually apply.
func printGrants(notef func(string, ...any), m component.Manifest) {
	for _, cmd := range m.Commands {
		notef("  adds     fft %s — %s", cmd.Name, cmd.Short)
		notef("           %s", grantOf(cmd))
		for _, id := range cmd.Claims {
			notef("           replaces the generated command for %s", id)
		}
	}
	for _, target := range m.Targets {
		notef("  delivers %s for the emulator", target)
	}
	for _, name := range m.Env {
		notef("  reads    $%s", name)
	}
}

// grantOf says in one line what a command will be given and what it may do.
func grantOf(cmd component.Command) string {
	switch cmd.Session {
	case component.SessionNone:
		return "no tenant credential"
	case component.SessionRead:
		return "a read-only tenant token"
	case component.SessionWrite:
		return "a tenant token, and it declares that it changes data"
	default:
		return fmt.Sprintf("session %s", cmd.Session)
	}
}
