package main

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// annotationOperationID is the key a command records the operation it calls under.
//
// It is what ties a cobra command back to the spec, and it does two jobs. It is
// what makes `--help` able to print the endpoint's purpose, its permissions and a
// sample body — none of which fft would otherwise know. And it is what makes
// *shadowing* work: an operation a curated command has claimed does not get a
// generated twin, so `fft facility list` stays the hand-written one and promoting an
// endpoint from Tier 2 to Tier 1 is a pure upgrade rather than a rename.
const annotationOperationID = "operationId"

// annotationGenerated marks a command as one auto-registered from the spec, so a
// spec can tell a curated command from its generated sibling without guessing from
// the name.
const annotationGenerated = "generated"

// helpWidth is where the prose in a help block wraps. 80 is what a terminal is,
// still, when someone has two of them side by side.
const helpWidth = 80

// installHelp gives the whole command tree a help function that knows about the
// spec.
//
// Cobra inherits a help function from the nearest ancestor that has one, so setting
// it on the root covers all 550-odd commands. A command with no operationId gets
// cobra's own help, unchanged — this adds a section, it does not replace the help.
func installHelp(deps *Deps, root *cobra.Command) {
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		out := cmd.OutOrStdout()

		if cmd.Long != "" {
			fmt.Fprintln(out, strings.TrimSpace(cmd.Long))
		} else if cmd.Short != "" {
			fmt.Fprintln(out, cmd.Short)
		}

		if op, ok := operationOf(cmd); ok {
			fmt.Fprintln(out)

			// The same block `fft api describe` renders, and coloured the same way. Help
			// can run before PersistentPreRunE has built the printer (cobra answers
			// `--help` on the root without one), so an absent printer means no colour —
			// which is also the right answer for a pipe.
			style := output.Style{}
			if deps.Printer != nil {
				style = deps.Printer.Style()
			}
			writeOperation(out, cmd.Root(), op, style)
		}

		fmt.Fprintln(out)
		fmt.Fprint(out, cmd.UsageString())
	})
}

// operationOf returns the operation a command calls, if it declares one.
func operationOf(cmd *cobra.Command) (api.Operation, bool) {
	id, ok := cmd.Annotations[annotationOperationID]
	if !ok {
		return api.Operation{}, false
	}
	return api.LookupOperation(id)
}

// writeOperation renders what the spec knows about an operation: what it is for,
// what it costs you in permissions, what it takes, and a body you can send.
//
// The same block backs `--help` and `fft api describe`, because they are the same
// question asked twice.
func writeOperation(out io.Writer, root *cobra.Command, op api.Operation, style output.Style) {
	label := func(s string) string { return style.Bold(s) }

	fmt.Fprintf(out, "%s\n  %s %s\n", label("ENDPOINT"), op.Method, op.Path)
	if op.Deprecated {
		fmt.Fprintf(out, "  %s\n", style.Yellow("This operation is deprecated."))
	}

	if purpose := purposeOf(op); purpose != "" {
		fmt.Fprintf(out, "\n%s\n%s\n", label("PURPOSE"), indent(wrap(purpose, helpWidth-2), "  "))
	}

	if len(op.Permissions) > 0 {
		fmt.Fprintf(out, "\n%s\n  %s\n", label("PERMISSIONS"), strings.Join(op.Permissions, ", "))
	}

	if len(op.Params) > 0 {
		fmt.Fprintf(out, "\n%s\n", label("PARAMETERS"))
		for _, p := range op.Params {
			fmt.Fprintf(out, "  %s\n", describeSpecParam(p, style))
		}
	}

	if op.SampleBody != "" {
		fmt.Fprintf(out, "\n%s\n%s\n", label("EXAMPLE BODY"), indent(strings.TrimRight(op.SampleBody, "\n"), "  "))
	}

	fmt.Fprintf(out, "\n%s\n%s\n", label("EXAMPLES"), indent(examplesFor(root, op), "  "))
}

// purposeOf is what the endpoint is for: the summary and the description, without
// repeating one in the other — for many operations the two are the same sentence.
func purposeOf(op api.Operation) string {
	switch {
	case op.Description == "":
		return op.Summary
	case op.Summary == "", strings.HasPrefix(op.Description, op.Summary):
		return op.Description
	default:
		return op.Summary + ". " + op.Description
	}
}

// describeSpecParam is one line of the PARAMETERS block.
func describeSpecParam(p api.Param, style output.Style) string {
	var b strings.Builder

	fmt.Fprintf(&b, "--%s", kebab(p.Name))

	kind := string(p.Type)
	if p.Type == api.TypeArray {
		// The encoding is the one thing about an array parameter a caller cannot guess,
		// and getting it wrong is silent. So --help says which one it is.
		encoding := "comma-joined"
		if p.Explode {
			encoding = "repeated"
		}
		kind = fmt.Sprintf("%s of %s, %s", p.Type, p.Item, encoding)
	}
	fmt.Fprintf(&b, " (%s, %s", p.In, kind)

	if p.Required {
		b.WriteString(", required")
	}
	b.WriteString(")")

	if len(p.Enum) > 0 {
		fmt.Fprintf(&b, ": one of %s", strings.Join(p.Enum, ", "))
	}
	if p.Description != "" {
		fmt.Fprintf(&b, " — %s", style.Faint(truncateAt(p.Description, 120)))
	}
	return b.String()
}

// examplesFor writes the two invocations that reach an operation: the generated
// command and the escape hatch. They are shown together because they are not
// interchangeable — the generated one has real flags, and `fft api` works even when
// the spec has an operation this build of fft has never heard of.
func examplesFor(root *cobra.Command, op api.Operation) string {
	var lines []string

	if cmd := commandPath(root, op); cmd != "" {
		lines = append(lines, cmd+exampleFlags(op, true))
	}
	lines = append(lines, "fft api "+op.ID+exampleFlags(op, false))

	if op.SampleBody != "" {
		lines = append(lines,
			fmt.Sprintf("fft api %s --example > body.json && fft api %s --file body.json", op.ID, op.ID))
	}
	return strings.Join(lines, "\n")
}

// exampleFlags is the required parameters, spelled the way the tier in question
// spells them: --pick-job-id X for a generated command, --param pickJobId=X for the
// escape hatch.
func exampleFlags(op api.Operation, typed bool) string {
	var b strings.Builder

	for _, p := range op.Params {
		if !p.Required {
			continue
		}

		value := "<" + p.Name + ">"
		if len(p.Enum) > 0 {
			value = p.Enum[0]
		}

		switch {
		case typed:
			fmt.Fprintf(&b, " --%s %s", kebab(p.Name), value)
		case p.In == api.InQuery:
			fmt.Fprintf(&b, " --query %s=%s", p.Name, value)
		case p.In == api.InHeader:
			fmt.Fprintf(&b, " --header %s=%s", p.Name, value)
		default:
			fmt.Fprintf(&b, " --param %s=%s", p.Name, value)
		}
	}

	if op.BodyRequired {
		b.WriteString(" --file body.json")
	}
	return b.String()
}

// wrap breaks prose at width columns, on word boundaries.
func wrap(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}

	var (
		b    strings.Builder
		line = words[0]
	)
	for _, word := range words[1:] {
		if len(line)+1+len(word) > width {
			b.WriteString(line)
			b.WriteByte('\n')
			line = word
			continue
		}
		line += " " + word
	}
	b.WriteString(line)
	return b.String()
}

// indent prefixes every line, including the empty ones a sample body has none of
// but a description might.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// truncateAt cuts a description that would otherwise wrap a help line three times.
//
// n is a byte count, so the cut is walked back to a rune boundary: the spec's prose
// is full of curly quotes and em-dashes, and slicing one in half emits a broken rune
// into --help.
func truncateAt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return strings.TrimSpace(s[:n]) + "…"
}
