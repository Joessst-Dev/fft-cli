package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/skill"
)

const skillShowLong = `Print the agent skill.

For an assistant that reads a single context file rather than a directory of
skills:

  fft skill show >> AGENTS.md

The markdown goes to stdout exactly as it is, --output and all: it is a document,
not a record, and the point of this command is to redirect it somewhere. Use
-o json to get the same thing wrapped with its name and description, for a tool
that wants to read those.`

// skillDocView is the skill as a record, for the caller who asked for one.
//
// Content is the whole file, frontmatter included, because that is what an agent
// has to be given — and Name and Description are lifted out of it so that a tool
// deciding *whether* to install the skill does not have to parse markdown to find
// out what it is for.
type skillDocView struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Content     string `json:"content" yaml:"content"`
}

func newSkillShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the skill's markdown to stdout",
		Long:  skillShowLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			return runSkillShow(deps)
		},
	}
}

func runSkillShow(deps *Deps) error {
	doc := skill.Document()

	if deps.Printer.Format() == output.Table {
		// Not Render: there is no table here and there is no record. `fft skill show`
		// is `cat`, and a `cat` that reformatted its input would be useless — the same
		// reason `fft auth token` writes to Out() rather than rendering.
		_, err := io.WriteString(deps.Printer.Out(), doc)
		return err
	}

	meta, _, err := skill.Parse(doc)
	if err != nil {
		// The embedded skill's own frontmatter, which a spec parses on every build. If
		// this fails, the binary was built from a broken tree.
		return fmt.Errorf("the embedded skill is malformed: %w", err)
	}

	return deps.Printer.Render(output.Rows{}, skillDocView{
		Name:        meta.Name,
		Description: meta.Description,
		Content:     doc,
	})
}
