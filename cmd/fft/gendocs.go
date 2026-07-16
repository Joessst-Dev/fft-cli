package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/docsmd"
)

// newGenDocsCmd writes the Markdown CLI reference the documentation site renders.
//
// It is hidden and makes no request: it walks a freshly built command tree and
// prints it. `make docs` runs it, and CI fails the build if its output is not
// already committed — the same no-drift contract `make generate` has. It is
// hand-rolled rather than built on cobra/doc because that package's man-page half
// pulls go-md2man into the binary, and because owning the format lets the pages
// carry a VitePress title and drop the dated "auto generated" footer that would
// make every run a diff.
func newGenDocsCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:    "gen-docs <dir>",
		Short:  "Generate the Markdown CLI reference (internal)",
		Hidden: true,
		Args:   usageArgs(cobra.ExactArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			return generateDocs(deps, args[0])
		},
	}
}

// generateDocs renders every curated command under dir, one file each. The
// directory is wiped first so a renamed or removed command leaves no orphan page
// behind — an orphan would pass the drift gate (the file is unchanged) while the
// site kept advertising a command that no longer exists.
func generateDocs(deps *Deps, dir string) error {
	root := newRootCmd(deps)
	pruneGenerated(root)

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	var walk func(cmd *cobra.Command) error
	walk = func(cmd *cobra.Command) error {
		if cmd.Hidden || cmd.Name() == "help" {
			return nil
		}
		if err := writeCommandPage(dir, cmd); err != nil {
			return err
		}
		for _, child := range cmd.Commands() {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

// pruneGenerated strips the Tier-2 generated commands from the tree so the
// reference documents only the hand-written surface. The ~570 generated
// operations are reachable and documented through `fft api` and the Discovery
// guide; a page each would bury the curated commands the reference exists for.
//
// Generated commands are mixed into the curated resource parents (fft facility
// carries the hand-written `list` next to the generated `get-all-facilities`), so
// this removes them by their annotation wherever they hang, then drops any resource
// group left empty — the tag-derived groups (picking, handover, …) that hold
// nothing but generated commands.
func pruneGenerated(root *cobra.Command) {
	for _, parent := range root.Commands() {
		for _, child := range parent.Commands() {
			if child.Annotations[annotationGenerated] == "true" {
				parent.RemoveCommand(child)
			}
		}
	}
	for _, child := range root.Commands() {
		if child.GroupID == groupResource && len(child.Commands()) == 0 {
			root.RemoveCommand(child)
		}
	}
}

// writeCommandPage renders one command to <dir>/<path>.md, where <path> is the
// command path with spaces turned to underscores — the convention cobra/doc uses,
// so intra-reference links stay predictable.
func writeCommandPage(dir string, cmd *cobra.Command) error {
	path := cmd.CommandPath()
	name := strings.ReplaceAll(path, " ", "_")

	var b strings.Builder

	fmt.Fprintf(&b, "---\ntitle: %s\n---\n\n", path)
	fmt.Fprintf(&b, "# %s\n\n", path)

	if cmd.Short != "" {
		fmt.Fprintf(&b, "%s\n\n", cmd.Short)
	}
	if long := strings.TrimSpace(cmd.Long); long != "" && long != cmd.Short {
		fmt.Fprintf(&b, "%s\n\n", long)
	}

	fmt.Fprintf(&b, "## Usage\n\n```\n%s\n```\n\n", strings.TrimSpace(cmd.UseLine()))

	if ex := strings.TrimSpace(cmd.Example); ex != "" {
		fmt.Fprintf(&b, "## Examples\n\n```sh\n%s\n```\n\n", ex)
	}

	if local := cmd.NonInheritedFlags().FlagUsages(); strings.TrimSpace(local) != "" {
		fmt.Fprintf(&b, "## Flags\n\n```\n%s```\n\n", local)
	}

	if children := documentedChildren(cmd); len(children) > 0 {
		b.WriteString("## Subcommands\n\n")
		for _, child := range children {
			fmt.Fprintf(&b, "- [%s](./%s.md)", child.CommandPath(),
				strings.ReplaceAll(child.CommandPath(), " ", "_"))
			if child.Short != "" {
				fmt.Fprintf(&b, " — %s", child.Short)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if cmd.HasParent() {
		b.WriteString("## See also\n\n")
		parent := cmd.Parent()
		fmt.Fprintf(&b, "- [%s](./%s.md) — parent command\n\n", parent.CommandPath(),
			strings.ReplaceAll(parent.CommandPath(), " ", "_"))
	}

	// Global flags live on the root page; repeating them on every page would be
	// noise, and hardcoding their names here would be a drift the gate can't see.
	if cmd.HasParent() && strings.TrimSpace(cmd.InheritedFlags().FlagUsages()) != "" {
		b.WriteString("> This command also accepts the [global flags](./fft.md#flags).\n")
	}

	return os.WriteFile(filepath.Join(dir, name+".md"), []byte(docsmd.EscapeAngles(b.String())), 0o600)
}

// documentedChildren are the subcommands a page links to, in a stable order: the
// visible ones, minus cobra's own help command.
func documentedChildren(cmd *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, child := range cmd.Commands() {
		if child.Hidden || child.Name() == "help" {
			continue
		}
		out = append(out, child)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
