package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const apiListLong = `List the operations of the fulfillmenttools API.

The list comes from the spec, not from the network: it works offline and needs no
project. 557 operations is more than a screen, so filter.

  fft api list --tag picking
  fft api list --search pickjob
  fft api list --tag "Stocks (Inventory)" -o json | jq -r '.[].id'

--tag matches a tag by substring, case-insensitively, so "picking" finds
"Picking (Operations)". --search matches the operationId, the path and the
summary.`

// operationView is one row of `fft api list`, and the whole document of
// `fft api describe`.
//
// This is fft's own shape, not the API's, so -o json renders the struct rather
// than passing bytes through: there are no API bytes here to pass.
type operationView struct {
	ID          string      `json:"id" yaml:"id"`
	Method      string      `json:"method" yaml:"method"`
	Path        string      `json:"path" yaml:"path"`
	Summary     string      `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string      `json:"description,omitempty" yaml:"description,omitempty"`
	Tags        []string    `json:"tags,omitempty" yaml:"tags,omitempty"`
	Permissions []string    `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Params      []api.Param `json:"params,omitempty" yaml:"params,omitempty"`
	HasBody     bool        `json:"hasBody" yaml:"hasBody"`
	SampleBody  string      `json:"sampleBody,omitempty" yaml:"sampleBody,omitempty"`
	Deprecated  bool        `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Command     string      `json:"command,omitempty" yaml:"command,omitempty"`
}

func newAPIListCmd(deps *Deps) *cobra.Command {
	var (
		tag    string
		search string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the API's operations",
		Long:  apiListLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error {
			ops := api.Operations()
			if tag != "" {
				ops = api.OperationsByTag(tag)
			}

			var matched []api.Operation
			for _, op := range ops {
				if matches(op, search) {
					matched = append(matched, op)
				}
			}

			if len(matched) == 0 {
				return deps.Printer.Empty("operations")
			}

			// The count is metadata, not data: it goes to stderr, so that
			// `fft api list -o json | jq` sees JSON and nothing else.
			deps.Printer.Notef("%d operations.", len(matched))

			if deps.Printer.Format() == output.Table {
				// The table needs no view models, and building them is not free: each one
				// resolves its command by walking all ~560 commands, so `fft api list` with no
				// filter would visit a third of a million nodes to render four columns.
				return deps.Printer.Render(listRows(deps, matched), nil)
			}
			return deps.Printer.Render(output.Rows{}, views(cmd.Root(), matched))
		},
	}

	f := cmd.Flags()
	f.StringVar(&tag, "tag", "", "Only operations with this tag (substring, case-insensitive)")
	f.StringVar(&search, "search", "", "Only operations whose id, path or summary contains this")

	if err := cmd.RegisterFlagCompletionFunc("tag", completeTag); err != nil {
		panic("register --tag completion: " + err.Error())
	}

	return cmd
}

func completeTag(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return api.Tags(), cobra.ShellCompDirectiveNoFileComp
}

// matches reports whether an operation answers a --search term.
func matches(op api.Operation, term string) bool {
	if term == "" {
		return true
	}

	needle := strings.ToLower(strings.TrimSpace(term))
	return strings.Contains(strings.ToLower(op.ID), needle) ||
		strings.Contains(strings.ToLower(op.Path), needle) ||
		strings.Contains(strings.ToLower(op.Summary), needle)
}

func listRows(deps *Deps, ops []api.Operation) output.Rows {
	style := deps.Printer.Style()

	rows := make([][]string, 0, len(ops))
	for _, op := range ops {
		summary := op.Summary
		if op.Deprecated {
			summary = strings.TrimSpace(style.Yellow("(deprecated)") + " " + summary)
		}

		rows = append(rows, []string{
			op.ID,
			op.Method,
			op.Path,
			field(style, summary),
		})
	}

	return output.Rows{
		Headers: []string{"OPERATION", "METHOD", "PATH", "SUMMARY"},
		Rows:    rows,
	}
}

func views(root *cobra.Command, ops []api.Operation) []operationView {
	out := make([]operationView, 0, len(ops))
	for _, op := range ops {
		out = append(out, view(root, op))
	}
	return out
}

func view(root *cobra.Command, op api.Operation) operationView {
	return operationView{
		ID:          op.ID,
		Method:      op.Method,
		Path:        op.Path,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		Permissions: op.Permissions,
		Params:      op.Params,
		HasBody:     op.HasBody,
		SampleBody:  op.SampleBody,
		Deprecated:  op.Deprecated,
		Command:     commandPath(root, op),
	}
}
