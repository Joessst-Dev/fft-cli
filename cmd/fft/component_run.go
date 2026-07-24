package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// annotationComponent marks a command that dispatches to a component, and names it.
//
// The read-only gate reads it: a component command carries no operationId — its
// requests are the component's, not fft's — so [Deps.guard] would wave it through
// on the strength of a missing annotation. This is what gives the gate something to
// look at. See [Deps.guardComponent].
const annotationComponent = "component"

// annotationComponentMutates records the manifest's own claim about whether the
// command changes the tenant. It is a string because cobra annotations are, and it
// is separate from the operationId annotation because the two answer to different
// authorities: one is the spec, the other is a manifest fft is choosing to believe.
const annotationComponentMutates = "componentMutates"

// annotationClaims lists the operationIds a component command supersedes, comma
// separated.
//
// It is a second annotation rather than a reuse of annotationOperationID because
// the two mean different things to two different readers. [claimedOperations] reads
// both, which is what suppresses the generated twins. [Deps.guard] reads only the
// first, because an operationId annotation is a promise that *fft* builds that
// request — and for a component, fft builds nothing.
const annotationClaims = "componentClaims"

// openComponents resolves the component registry.
//
// Every failure here is swallowed into an empty registry, and it has to be: this
// runs while the command tree is being built, before flags are parsed, before the
// printer exists and before there is any stream to report an error on. A machine
// whose home directory cannot be resolved gets an fft with no components rather than
// an fft that will not start.
func (d *Deps) openComponents() {
	if d.Components != nil {
		return
	}

	root, enabled, err := component.Root(os.LookupEnv)
	if err != nil || !enabled {
		d.Components = component.Open("")
		return
	}
	d.Components = component.Open(root)
}

// flushComponentWarnings reports whatever registering the components had to say,
// once, on stderr.
//
// Warnings and not errors: fft still works, one command of one component is missing
// from the tree, and only the user can decide what to do about that.
//
// It is called from two places, and needs to be. [Deps.complete] covers every
// command that runs — but cobra answers --help *before* PersistentPreRunE, and
// `fft --help` is precisely where someone notices that a command they installed is
// not there. Emptying the slice is what keeps the two from both printing it.
func (d *Deps) flushComponentWarnings(w io.Writer) {
	for _, warning := range d.componentWarnings {
		fmt.Fprintf(w, "Warning: %s\n", warning)
	}
	d.componentWarnings = nil
}

// addComponentCommands registers a command for everything the installed components
// declare.
//
// It runs after the curated commands and before the generated ones, which is what
// makes a component's `claims` work: [claimedOperations] walks the tree for
// operationId annotations, so an operation a component claims is already spoken for
// by the time [addGeneratedCommands] looks. A component is therefore a Tier-1
// command written by somebody else, with exactly the shadowing a curated one has.
//
// A component may not take a name a built-in command already has. Ordering alone
// would let it — cobra would keep both and resolve to one of them — and a component
// that could shadow `fft facility` is a component that can silently change what a
// script does. The built-in wins, and the collision is reported on stderr rather
// than swallowed, because a component whose command never appears is otherwise a
// mystery.
func addComponentCommands(deps *Deps, root *cobra.Command) {
	if deps.Components == nil {
		return
	}

	// The names a component may not take. Two sources, because addGeneratedCommands
	// has not run yet: the curated commands already in the tree (found by root.Find),
	// and the resource-group names the generated step is *about* to create. Without
	// the second, a component named `picking` would slip past this check now and then
	// be silently adopted as the parent of the generated picking group — breaking both
	// its own "everything reaches me untouched" contract and the generated commands'
	// help. Reserved here so the collision is refused, with a reason, instead.
	reserved := generatedGroupNames()

	for _, c := range deps.Components.All() {
		if c.Kind != component.KindCommand {
			continue
		}

		for _, spec := range c.Commands {
			existing, _, findErr := root.Find([]string{spec.Name})
			taken := (findErr == nil && existing != nil && existing != root) || reserved[spec.Name]
			if taken {
				// Kept rather than printed: this runs while the tree is being built, before
				// the flags are parsed and before there is a printer to write it with.
				// [Deps.complete] flushes it once there is. A collision that was silently
				// dropped would leave the user with an installed component whose command
				// simply never appears, and nothing anywhere saying why.
				deps.componentWarnings = append(deps.componentWarnings, fmt.Sprintf(
					"the %s component declares a command called %q, which fft already has; ignoring it",
					c.Name, spec.Name))
				continue
			}
			root.AddCommand(newComponentStub(deps, c, spec))
		}
	}
}

// generatedGroupNames is the set of resource-group names addGeneratedCommands will
// create — one per distinct tag across the spec. A component command may not take one
// of these, because the generated step reuses an existing same-named child as its
// group parent and would otherwise adopt a component stub.
//
// Every operation's group is reserved, claimed or not: a claim suppresses a single
// generated command, not the group, and over-reserving a name that a fully-claimed
// tag would not have created only forbids a component one more name — which fails
// safe, the way reservedFlags does.
func generatedGroupNames() map[string]bool {
	names := make(map[string]bool)
	for _, op := range api.Operations() {
		names[groupFor(op)] = true
	}
	return names
}

// newComponentStub is the cobra command that stands in for a component's.
//
// Flag parsing is off, so everything after the command name reaches the component
// untouched: a component owns its own flag syntax completely, and fft never has to
// be taught it. The cost is that --help goes to the component too — which is right
// when it is installed, since it knows its own flags, and handled below when it is
// not.
func newComponentStub(deps *Deps, c component.Component, spec component.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:                strings.TrimSpace(spec.Name + " [flags] [args]"),
		Short:              spec.Short,
		Long:               componentStubLong(c, spec),
		GroupID:            groupComponent,
		DisableFlagParsing: true,

		Annotations: map[string]string{
			annotationComponent:        c.Name,
			annotationComponentMutates: strconv.FormatBool(spec.Mutates),
			annotationClaims:           strings.Join(spec.Claims, ","),
		},

		RunE: func(cmd *cobra.Command, args []string) error {
			return runComponent(deps, cmd, c, spec, args)
		},
	}

	// Cobra's own completion of a component's flags is not something fft can offer:
	// it does not know them, and guessing would complete flags that do not exist.
	cmd.ValidArgsFunction = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return cmd
}

// componentStubLong is the help a stub shows: the component's own description, plus
// the two facts fft knows and the component does not say — who wrote it, and what
// fft will hand it.
func componentStubLong(c component.Component, spec component.Command) string {
	var b strings.Builder

	if spec.Long != "" {
		b.WriteString(strings.TrimSpace(spec.Long))
	} else {
		b.WriteString(spec.Short)
	}
	b.WriteString("\n\n")

	origin := fmt.Sprintf("Provided by the %s component", c.Name)
	if c.Source != "" {
		origin += " (" + c.Source + ")"
	}
	b.WriteString(origin + ", which runs as you.\n")
	b.WriteString("fft gives it " + grantOf(spec) + ".\n")

	if !c.Installed {
		b.WriteString("\nIt is not installed. Run 'fft component install " + c.Name + "'.\n")
	}
	return b.String()
}

// runComponent dispatches to the component's executable.
func runComponent(deps *Deps, cmd *cobra.Command, c component.Component, spec component.Command, args []string) error {
	if !c.Installed {
		// --help on a component that is not installed is a question fft can answer
		// itself, and a better answer than the error: it says what the thing is, and
		// then how to get it.
		if slices.ContainsFunc(args, isHelpFlag) {
			return cmd.Help()
		}
		return &component.NotInstalledError{Name: c.Name, Command: cmd.CommandPath()}
	}

	// The command's own context, not deps.Context — a component must not inherit
	// --timeout. That flag bounds a single API call, and a component is not one: the
	// emulator runs until Ctrl-C, and a 30-second default deadline would kill it
	// mid-serve. The context still carries the signal cancellation the root installs,
	// so Ctrl-C drains the child cleanly; how long the child runs is the child's own
	// affair, and any deadline it wants is a flag it parses itself.
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	env, err := deps.componentEnv(ctx, c, spec)
	if err != nil {
		return err
	}

	return component.Run(ctx, c, args, env, component.Streams{
		In:  cmd.InOrStdin(),
		Out: cmd.OutOrStdout(),
		Err: cmd.ErrOrStderr(),
	})
}

func isHelpFlag(arg string) bool { return arg == "-h" || arg == "--help" }

// componentEnv builds the environment a component runs with, resolving the tenant
// session only when the manifest asks for one.
//
// The order matters: a component declaring [component.SessionNone] must not cause a
// keychain to be opened or a token to be minted. The emulator is the case that
// proves it — it serves a fake tenant, and running it should not make fft sign in to
// a real one.
func (d *Deps) componentEnv(ctx context.Context, c component.Component, spec component.Command) ([]string, error) {
	opts := component.EnvOptions{
		Root:    d.Components.Root(),
		Version: buildinfo.Version,
		NoColor: !d.Printer.Style().Enabled(),
		Timeout: d.Timeout,
	}
	if format := d.Printer.Format(); format != output.Table {
		opts.Output = string(format)
	}

	if spec.Session != component.SessionNone {
		session, err := d.componentSession(ctx)
		if err != nil {
			return nil, err
		}
		opts.Session = session
	}

	return component.Environ(os.Environ(), c, spec, opts)
}

// componentSession resolves the tenant session a component is to be given: the
// active project, and a freshly minted id token for it.
//
// The token comes from fft's own token source, which is what keeps the keychain
// something only fft opens. A component receives a credential that expires; it never
// receives the one that mints more.
func (d *Deps) componentSession(ctx context.Context) (*component.SessionInfo, error) {
	p, src, err := d.tokenSource()
	if err != nil {
		return nil, err
	}

	token, err := src.Token(ctx)
	if err != nil {
		return nil, err
	}

	return &component.SessionInfo{
		BaseURL:     p.BaseURL,
		Email:       p.Email,
		Token:       token,
		Tenant:      p.Tenant,
		ProjectID:   p.ProjectID,
		Environment: p.Environment,
		ReadOnly:    p.ReadOnly || d.ReadOnlyEnv || (d.ReadOnlyFlag != nil && *d.ReadOnlyFlag),
	}, nil
}
