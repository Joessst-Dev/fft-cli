package main

import (
	"slices"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/template"
)

const templateListLong = `List the templates fft can see.

Both scopes are read: ./.fft/templates first, then your own. Exactly the templates
a name would resolve to are listed, one row each — a user template hidden by a
project template of the same name is reported on stderr instead, so that stdout is
the set 'fft template render' would actually reach.`

// templateRow is one row of the list. It is fft's own shape, not a document the
// API produced, so -o json renders this rather than a file's bytes.
type templateRow struct {
	Name        string         `json:"name" yaml:"name"`
	Scope       template.Scope `json:"scope" yaml:"scope"`
	OperationID string         `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Project     string         `json:"project,omitempty" yaml:"project,omitempty"`
	Params      []string       `json:"params,omitempty" yaml:"params,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Path        string         `json:"path" yaml:"path"`
}

func newTemplateListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List saved templates",
		Long:    templateListLong,
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			store, err := templateStore()
			if err != nil {
				return err
			}

			listing, err := store.List()
			if err != nil {
				return err
			}
			reportTemplateProblems(deps, listing.Problems)

			if len(listing.Shadowed) > 0 {
				names := make([]string, len(listing.Shadowed))
				for i, saved := range listing.Shadowed {
					names[i] = saved.Name
				}
				deps.Printer.Notef(
					"Hidden by a project template of the same name: %s.", quotedNames(names))
			}

			if len(listing.Found) == 0 {
				return deps.Printer.Empty("templates")
			}

			rows := make([]templateRow, len(listing.Found))
			for i, saved := range listing.Found {
				rows[i] = templateRow{
					Name:        saved.Name,
					Scope:       saved.Scope,
					OperationID: saved.OperationID,
					Project:     saved.Project,
					Params:      paramNames(saved),
					Description: saved.Description,
					Path:        saved.Path,
				}
			}

			return deps.Printer.Render(templateTable(deps, rows), rows)
		},
	}
}

func templateTable(deps *Deps, rows []templateRow) output.Rows {
	style := deps.Printer.Style()

	table := output.Rows{Headers: []string{"NAME", "SCOPE", "OPERATION", "PROJECT", "DESCRIPTION"}}
	for _, r := range rows {
		// OperationID, Project and Description come from the template file, which
		// for the project scope arrives via git clone — untrusted relative to
		// whoever is running list to survey what fft can see before trusting any
		// of it. output.Sanitize keeps a crafted file from using a control byte to
		// rewrite or hide what was already printed, the same as template_show.go.
		table.Rows = append(table.Rows, []string{
			r.Name,
			string(r.Scope),
			field(style, output.Sanitize(r.OperationID)),
			field(style, output.Sanitize(r.Project)),
			field(style, output.Sanitize(r.Description)),
		})
	}
	return table
}

// paramNames lists a template's declared parameters, sorted, for the machine
// formats — it is the one thing a caller needs before it can render anything.
func paramNames(saved template.Saved) []string {
	if len(saved.Params) == 0 {
		return nil
	}
	names := make([]string, 0, len(saved.Params))
	for name := range saved.Params {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
