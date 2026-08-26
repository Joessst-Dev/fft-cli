package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/template"
)

const templateRemoveLong = `Delete a saved template.

Your own templates are removed by default; --local removes the project's. The two
are kept apart on purpose here, unlike everywhere else: deleting a template the
whole team committed should take saying so.

Nothing here reaches the tenant — the entities the template addressed are untouched.`

func newTemplateRemoveCmd(deps *Deps) *cobra.Command {
	var scope scopeFlag

	cmd := &cobra.Command{
		Use:               "remove <name>",
		Aliases:           []string{"rm", "delete"},
		Short:             "Delete a saved template",
		Long:              templateRemoveLong,
		Args:              usageArgs(cobra.ExactArgs(1)),
		ValidArgsFunction: completeTemplateNames,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if err := template.ValidateName(name); err != nil {
				return err
			}

			store, err := templateStore()
			if err != nil {
				return err
			}

			exists, err := store.Exists(name, scope.scope())
			if err != nil {
				return err
			}
			if !exists {
				if hint := templateScopeHint(store, name, scope.scope()); hint != nil {
					return hint
				}
				// Fall through to store.Remove below rather than construct the error
				// here: it already knows every template name for the "did you mean".
			}

			path, err := store.Path(name, scope.scope())
			if err != nil {
				return err
			}

			question := fmt.Sprintf("Remove the template %s (%s)?", name, path)
			if scope.scope() == template.ScopeProject {
				question = fmt.Sprintf(
					"Remove the project template %s (%s)? It may be committed to this repository.",
					name, path)
			}

			ok, err := confirmDestructive(deps, question)
			if err != nil {
				return err
			}
			if !ok {
				return exitcode.UsageError{Err: fmt.Errorf("cancelled")}
			}

			removed, err := store.Remove(name, scope.scope())
			if err != nil {
				return err
			}

			deps.Printer.Notef("Removed %s.", name)
			view := templatePathView{Template: name, Path: removed}
			return deps.Printer.Render(output.Rows{
				Headers: []string{"TEMPLATE", "PATH"},
				Rows:    [][]string{{view.Template, view.Path}},
			}, view)
		},
	}

	scope.register(cmd, "Remove from")
	return cmd
}
