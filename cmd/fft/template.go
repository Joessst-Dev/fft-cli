package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/template"
)

const templateLong = `Save a request body you send often, and change the parts that vary.

A template is a JSON file holding a body, the operation it was written for, and
the parameters that change between sends. ` + "`fft template render`" + ` prints the
finished body on stdout, so it composes with every command that takes --file:

    fft template render rush-order --set email=a@b.de | fft order create --file -

Nothing here reaches the tenant. Rendering makes no request, needs no project and
needs no credentials, which is why it is safe to put in the middle of a pipe and
why a read-only project cannot refuse it — the command that receives the body is
still gated exactly as it was.

Templates live in two places. ` + "`--local`" + ` writes ./.fft/templates, which is meant to
be committed and shared with whoever clones the repository; without it they go to
your own $XDG_DATA_HOME/fft/templates. A project template of the same name wins.

A body captured from real work carries real ids. Read a template before you commit
it — a facility id or a consumer email in git history is not something you can
quietly take back later.`

// newTemplateCmd builds `fft template`.
//
// It sits in the core group rather than among the resources: it addresses no
// operation, and it is local tooling in the same sense `fft skill` and
// `fft component` are.
func newTemplateCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Save request bodies and render them with parameters",
		Long:  templateLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newTemplateListCmd(deps),
		newTemplateShowCmd(deps),
		newTemplateRenderCmd(deps),
		newTemplateSaveCmd(deps),
		newTemplateRemoveCmd(deps),
	)

	return cmd
}

// templateStore resolves both template directories.
func templateStore() (*template.Store, error) {
	store, err := template.NewStore(nil)
	if err != nil {
		return nil, exitcode.UsageError{Err: err}
	}
	return store, nil
}

// scopeFlag is --local: write to, or read from, the project's directory instead
// of the user's.
//
// It is spelled --local because `fft skill install --local` already means exactly
// this, and a second word for one idea is a word to look up.
type scopeFlag struct{ local bool }

func (s *scopeFlag) register(cmd *cobra.Command, verb string) {
	cmd.Flags().BoolVar(&s.local, "local", false,
		fmt.Sprintf("%s ./.fft/templates, the directory this repository commits", verb))
}

func (s *scopeFlag) scope() template.Scope {
	if s.local {
		return template.ScopeProject
	}
	return template.ScopeUser
}

// activeProjectName is the project a template is recorded against, or compared
// with, or "" when there is none.
//
// It is deliberately not [Deps.ActiveProject]: that one opens the secret store
// for a Firebase key nothing here signs anything with, and returns exit 3 when no
// project is configured. A template is inert data — `fft template render` has to
// work on a machine that has never run `fft project add`, exactly as `--example`
// does — so a missing project is an empty string, not a failure.
func activeProjectName(deps *Deps) string {
	if deps.Ephemeral != nil && (deps.Project == "" || deps.Project == deps.Ephemeral.Name) {
		return deps.Ephemeral.Name
	}

	cfg, err := deps.LoadConfig()
	if err != nil {
		return ""
	}
	project, err := cfg.Resolve(deps.Project)
	if err != nil {
		if errors.Is(err, config.ErrNoActiveProject) || errors.Is(err, config.ErrProjectNotFound) {
			return ""
		}
		return ""
	}
	return project.Name
}

// warnProjectMismatch says so when a template was saved against another tenant.
//
// The ids in a saved body — facilities, consumers, orders — are the one thing
// that reliably does not survive the move between tenants, and a body that
// silently addresses the wrong one is worse than a body that fails. It is a
// warning and not an error because promoting a template from staging to
// production is a thing people legitimately do.
//
// Warnf writes to stderr, so this cannot reach the document on stdout.
func warnProjectMismatch(deps *Deps, saved template.Saved) {
	active := activeProjectName(deps)
	if saved.Project == "" || active == "" || saved.Project == active {
		return
	}
	deps.Printer.Warnf("%s was saved under project %q, and the active project is %q. "+
		"Ids in the body may not resolve there.", saved.Name, saved.Project, active)
}

// reportProblems names the files in a template directory fft could not read.
//
// One unparseable file must not make the whole command useless, and it must not
// be silent either — a template somebody edited into invalid JSON would otherwise
// simply stop appearing.
func reportTemplateProblems(deps *Deps, problems []template.Problem) {
	for _, p := range problems {
		deps.Printer.Warnf("ignoring %s: %v", p.Path, p.Err)
	}
}

// completeTemplateNames completes a template argument from what is on disk.
func completeTemplateNames(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	store, err := templateStore()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	listing, err := store.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	out := make([]string, 0, len(listing.Found))
	for _, saved := range listing.Found {
		description := saved.Description
		if description == "" {
			description = saved.OperationID
		}
		out = append(out, saved.Name+"\t"+description)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// operationSummary is what the spec says about a template's operation, and
// whether it still knows it at all.
//
// A regenerated spec that renamed an operationId must not make a saved body
// un-renderable: nothing is being sent, and the body is still the user's. So an
// operation fft cannot find is a warning here rather than the exit 2 that
// findOperation gives it.
func operationSummary(deps *Deps, saved template.Saved) {
	if saved.OperationID == "" {
		return
	}

	op, ok := api.LookupOperation(saved.OperationID)
	if !ok {
		deps.Printer.Warnf("this fft has no operation %q: the spec may have moved since %s was saved.",
			saved.OperationID, saved.Name)
		return
	}
	if !op.HasBody {
		deps.Printer.Warnf("%s takes no request body, so %s has nowhere to send one.",
			saved.OperationID, saved.Name)
	}
	if op.Deprecated {
		deps.Printer.Warnf("%s is deprecated.", saved.OperationID)
	}
}

// templateScopeHint tells a user who asked the wrong scope which flag reaches
// the other one, rather than reporting a template they can see as absent.
func templateScopeHint(store *template.Store, name string, want template.Scope) error {
	other := template.ScopeProject
	flag := "--local"
	if want == template.ScopeProject {
		other = template.ScopeUser
		flag = "no --local"
	}

	if exists, err := store.Exists(name, other); err == nil && exists {
		return exitcode.UsageError{Err: fmt.Errorf(
			"%q is a %s template, not a %s one: pass %s to reach it",
			name, other, want, flag)}
	}
	return nil
}

// quotedNames renders a list of names for a message.
func quotedNames(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(out, ", ")
}
