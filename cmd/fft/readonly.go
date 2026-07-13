package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// The read-only gate.
//
// A project can be marked read-only — in the config file, through FFT_READ_ONLY,
// or for one invocation with --read-only — and fft then refuses every request that
// would change the tenant, while every read keeps working. Which is not the same
// as "refuse the POSTs": the API's cursor searches are POSTs, and they are the read
// path behind every list command there is. [api.Operation.Mutates] is what knows
// the difference.
//
// The gate is keyed on annotationOperationID, the annotation every curated and
// every generated command already carries. That is the enforcement: not a
// transport that catches a mutating request on its way out, but an invariant that
// no such request is ever built. The tree-walk spec in readonly_test.go fails the
// build the day a command can reach the tenant without saying which operation it
// is, which is what makes the annotation something the gate may rely on.

// guard refuses a command that would change a read-only project.
//
// A command with no operationId annotation makes no tenant request — `fft version`,
// `fft project use`, `fft api describe` — and is never gated. The one command it
// cannot see this way is `fft api <operationId>`, whose operation is an argument
// rather than an annotation; it gates itself with [Deps.guardOperation].
func (d *Deps) guard(cmd *cobra.Command) error {
	id, ok := cmd.Annotations[annotationOperationID]
	if !ok {
		return nil
	}

	op, ok := api.LookupOperation(id)
	if !ok {
		// An annotation naming an operation the spec does not have is a build defect,
		// and the tree-walk spec exists to catch it. Until it does, the command must
		// not run: fft cannot say whether it writes, and guessing is the one thing
		// this whole file is here to avoid.
		return fmt.Errorf("%s is annotated with the operation %q, which the API spec does not have", cmd.CommandPath(), id)
	}
	return d.guardOperation(cmd, op)
}

// guardOperation refuses op when the project, the environment or the session is
// read-only.
//
// The order of the checks is the contract, and every step of it is load-bearing.
func (d *Deps) guardOperation(cmd *cobra.Command, op api.Operation) error {
	// A read is never gated — and it is answered first so that resolving the project
	// stays the business of the command that needs one. Otherwise `fft facility list`
	// with no active project would fail *here*, and this gate would become the thing
	// that decides what "no active project" means.
	if !op.Mutates() {
		return nil
	}

	// --example prints a sample request body from the spec. It needs no project, no
	// credential and no network — and it is read inside RunE, which this gate runs
	// before. Without this line, `fft api addPickJob --example` on a read-only
	// project would exit 10: a refusal to print an example. (--help needs no such
	// exemption: cobra answers it before PersistentPreRunE runs at all.)
	if exampleWanted(cmd) {
		return nil
	}

	// The project comes before the tighten-only check below, because whether
	// --read-only=false is *loosening* anything is a question only the project can
	// answer. So a command with no project at all fails with the usual config error
	// (exit 3) rather than being told about a flag — which is the right order: the
	// missing project is the thing wrong with that command line.
	p, err := d.ActiveProject()
	if err != nil {
		// The same config error the command itself would have raised a moment later.
		return err
	}

	source, blocked := d.readOnlySource(p)

	// Tighten-only. --read-only=false is not a request fft grants: a guardrail that a
	// flag can switch off is a guardrail that a copied-and-pasted command line
	// switches off. It is refused as a usage error rather than ignored, because
	// silently disregarding a flag the user typed is its own kind of lie.
	if d.ReadOnlyFlag != nil && !*d.ReadOnlyFlag {
		switch source {
		case sourceProject:
			return exitcode.UsageError{Err: fmt.Errorf(
				"--read-only=false cannot loosen project %q, which is configured read-only", p.Name)}
		case sourceEnv:
			return exitcode.UsageError{Err: fmt.Errorf(
				"--read-only=false cannot loosen %s, which is set", config.EnvReadOnly)}
		}
	}

	if !blocked {
		return nil
	}
	return &readOnlyError{op: op, project: p.Name, source: source}
}

// readOnlySource is where the refusal came from, and there may be more than one.
// It decides the hint, so it has to name the source whose remedy is the one the
// user can actually carry out.
//
// A configured project is reported first: it is the durable setting, and the one
// whose remedy is a decision rather than a keystroke. But an *ephemeral* project is
// only ever read-only because FFT_READ_ONLY made it so — it has no config entry, so
// telling its user to run `fft project read-only env --off` would send them looking
// for a project that exists nowhere.
func (d *Deps) readOnlySource(p config.Project) (readOnlySource, bool) {
	switch {
	case p.ReadOnly && !p.Ephemeral:
		return sourceProject, true
	case p.ReadOnly, d.ReadOnlyEnv:
		// An ephemeral project is read-only only because the environment said so.
		return sourceEnv, true
	case d.ReadOnlyFlag != nil && *d.ReadOnlyFlag:
		return sourceFlag, true
	default:
		return 0, false
	}
}

// exampleWanted reports whether the user asked for --example.
//
// Asked *for* it: --example=false is a flag given and a request not made, so the
// value is read rather than merely its Changed bit.
func exampleWanted(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup("example")
	return f != nil && f.Value.String() == "true"
}

// readOnlySource is what made the project read-only, which is what decides the hint.
type readOnlySource int

const (
	sourceProject readOnlySource = iota + 1
	sourceEnv
	sourceFlag
)

// readOnlyError is a write refused before it was sent.
//
// Its own exit code (10) rather than a Forbidden (5), because they are different
// facts about the world with different fixes: a 403 means the tenant refused the
// request, and this means fft never made it. Nothing left the machine, no token was
// minted, and trying again with better credentials will not help.
type readOnlyError struct {
	op      api.Operation
	project string
	source  readOnlySource
}

func (e *readOnlyError) Error() string {
	return fmt.Sprintf("project %q is read-only: %s (%s %s) would change data, and nothing was sent",
		e.project, e.op.ID, e.op.Method, e.op.Path)
}

func (e *readOnlyError) ExitCode() int { return exitcode.ReadOnly }

func (e *readOnlyError) Hint() string {
	switch e.source {
	case sourceEnv:
		return fmt.Sprintf("Unset %s to allow writes.", config.EnvReadOnly)
	case sourceFlag:
		return "Drop --read-only to allow writes."
	default:
		return fmt.Sprintf("Run 'fft project read-only %s --off' to allow writes.", e.project)
	}
}
