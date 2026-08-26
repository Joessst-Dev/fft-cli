package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/template"
)

const templateShowLong = `Describe a saved template: what it is for, what it sends, and what you can change.

This prints the whole template — the envelope, its declared parameters and the body
as saved. 'fft template render' prints only the body, with the parameters applied;
this is the one to read before you type a --set.

-o json and -o yaml print the file's own contents, so 'fft template show x -o json'
round-trips through 'fft template save x --file -'.`

func newTemplateShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:               "show <name>",
		Short:             "Describe a saved template",
		Long:              templateShowLong,
		Args:              usageArgs(cobra.ExactArgs(1)),
		ValidArgsFunction: completeTemplateNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := templateStore()
			if err != nil {
				return err
			}

			saved, err := store.Resolve(args[0])
			if err != nil {
				return err
			}

			warnProjectMismatch(deps, saved)
			warnShadowing(deps, store, saved)

			if deps.Printer.Format() != output.Table {
				// The file's own encoded bytes, not the Go Saved value: Encode already
				// key-sorts and, critically, never round-trips a json.Number through a
				// Go struct where -o yaml would quote it and -o json would re-marshal
				// it. RenderRaw re-indents rather than re-encoding, the same way a read
				// command avoids re-encoding the API's own document.
				raw, err := template.Encode(saved.Template)
				if err != nil {
					return err
				}
				return deps.Printer.RenderRaw(output.Rows{}, raw)
			}

			writeTemplate(cmd.OutOrStdout(), saved, deps.Printer.Style())
			return nil
		},
	}
}

// writeTemplate renders the labelled block `fft template show` prints, in the
// shape `fft api describe` uses for an operation — the same question about a
// different noun deserves the same answer layout.
func writeTemplate(out io.Writer, saved template.Saved, style output.Style) {
	label := func(s string) string { return style.Bold(s) }

	fmt.Fprintf(out, "%s\n  %s (%s scope)\n  %s\n",
		label("TEMPLATE"), saved.Name, saved.Scope, style.Faint(saved.Path))

	// Description, OperationID and Project come from the template file, which for
	// the project scope arrives via git clone — untrusted relative to whoever is
	// running show to read it before trusting it. output.Sanitize keeps a crafted
	// file from using a control byte to rewrite or hide what was already printed.
	if description := output.Sanitize(saved.Description); description != "" {
		fmt.Fprintf(out, "\n%s\n%s\n", label("PURPOSE"), indent(wrap(description, helpWidth-2), "  "))
	}

	if operationID := output.Sanitize(saved.OperationID); operationID != "" {
		fmt.Fprintf(out, "\n%s\n  %s", label("OPERATION"), operationID)
		if op, ok := api.LookupOperation(saved.OperationID); ok {
			fmt.Fprintf(out, "  %s %s", op.Method, op.Path)
		} else {
			fmt.Fprintf(out, "  %s", style.Yellow("(this fft does not know it)"))
		}
		fmt.Fprintln(out)
	}

	if project := output.Sanitize(saved.Project); project != "" {
		fmt.Fprintf(out, "\n%s\n  %s\n", label("SAVED UNDER"), project)
	}

	if names := paramNames(saved); len(names) > 0 {
		fmt.Fprintf(out, "\n%s\n", label("PARAMETERS"))

		width := 0
		for _, name := range names {
			width = max(width, len(name))
		}
		for _, name := range names {
			fmt.Fprintf(out, "  %s\n", describeTemplateParam(name, saved.Params[name], width, style))
		}
	}

	if body, err := json.MarshalIndent(saved.Body, "", "  "); err == nil {
		fmt.Fprintf(out, "\n%s\n%s\n", label("BODY"), indent(string(body), "  "))
	}
}

// describeParam is one line of the PARAMETERS block: what to call it, where it
// goes, and whether the template will refuse to render without it.
func describeTemplateParam(name string, p template.Param, width int, style output.Style) string {
	var b strings.Builder
	b.WriteString(style.Bold(output.Sanitize(name)))
	fmt.Fprintf(&b, "%s  %s", strings.Repeat(" ", width-len(name)), style.Faint(output.Sanitize(p.Path)))

	switch {
	case p.Required:
		fmt.Fprintf(&b, "  %s", style.Yellow("required"))
	case p.Default != nil:
		if def, err := json.Marshal(p.Default); err == nil {
			fmt.Fprintf(&b, "  default %s", string(def))
		}
	}

	if description := output.Sanitize(p.Description); description != "" {
		fmt.Fprintf(&b, "\n    %s", style.Faint(description))
	}
	return b.String()
}
