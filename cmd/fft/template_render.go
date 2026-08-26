package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
	"github.com/Joessst-Dev/fft-cli/internal/template"
)

const templateRenderLong = `Print a saved body with its parameters filled in.

The body goes to stdout and everything else — warnings, the picker, a project
mismatch — goes to stderr, so this composes:

    fft template render rush-order --set email=a@b.de | fft order create --file -

--set takes either a parameter the template declares or a path into the body:

    --set email=a@b.de                      a declared parameter
    --set order.consumer.email=a@b.de       the same place, spelled out
    --set order.items.0.quantity=3          an array element
    --set order.items.1.quantity=1          one past the end appends

A value that parses as JSON is that JSON, so 3 is a number, true is a boolean and
{"a":1} is an object. Anything else is a string. --set-string skips that reading
entirely, which is what an id made only of digits needs: --set-string id=12345
sends "12345" where --set id=12345 would send 12345.

Rendering makes no request. It needs no project, no credentials and no network,
and it always prints JSON — even under -o yaml — because the body is going into a
command that reads JSON.

Called with no name on a terminal, it asks which template to render.`

func newTemplateRenderCmd(deps *Deps) *cobra.Command {
	var sets setFlags

	cmd := &cobra.Command{
		Use:               "render [name]",
		Short:             "Print a saved body with its parameters filled in",
		Long:              templateRenderLong,
		Args:              usageArgs(cobra.MaximumNArgs(1)),
		ValidArgsFunction: completeTemplateNames,
		RunE: func(_ *cobra.Command, args []string) error {
			store, err := templateStore()
			if err != nil {
				return err
			}

			name, err := templateName(deps, store, args)
			if err != nil {
				return err
			}

			saved, err := store.Resolve(name)
			if err != nil {
				return err
			}

			overrides, err := sets.parse()
			if err != nil {
				return err
			}

			body, err := template.Render(saved.Template, overrides)
			if err != nil {
				return renderError(err)
			}

			// Every warning is emitted before the body, so that a human watching a
			// terminal reads them above the thing they are about.
			operationSummary(deps, saved)
			warnProjectMismatch(deps, saved)
			warnShadowing(deps, store, saved)

			// Not Printer.RenderDocument: that honours -o yaml, and YAML is not
			// something `--file -` can read. The output of this command is a
			// request body, not a view of one.
			_, err = fmt.Fprint(deps.Printer.Out(), string(body))
			return err
		},
	}

	f := cmd.Flags()
	f.Var(sets.value(false), "set",
		"Set a parameter or a path: --set email=a@b.de (repeatable)")
	f.Var(sets.value(true), "set-string",
		"Set a value as a string, whatever it looks like: --set-string id=12345 (repeatable)")

	return cmd
}

// setFlags collects --set and --set-string in the single order pflag actually
// parses them in.
//
// template.Render's "last one for a path wins" has to mean the order the two
// flags were interleaved on the command line, not "every --set-string after
// every --set" — which is what reading each flag into its own []string and
// concatenating them would silently give you.
type setFlags struct {
	raw []rawSet
}

type rawSet struct {
	arg      string
	asString bool
}

// value is a pflag.Value that appends into the shared, order-preserving raw
// slice. --set and --set-string both get one of these, over the same setFlags,
// so pflag records both flags' occurrences in one real timeline.
func (s *setFlags) value(asString bool) pflag.Value {
	return &setFlagValue{sets: s, asString: asString}
}

type setFlagValue struct {
	sets     *setFlags
	asString bool
}

func (v *setFlagValue) String() string { return "" }
func (v *setFlagValue) Type() string   { return "stringArray" }

func (v *setFlagValue) Set(arg string) error {
	v.sets.raw = append(v.sets.raw, rawSet{arg: arg, asString: v.asString})
	return nil
}

// parse turns the collected --set/--set-string arguments into overrides, in the
// order they were typed.
func (s *setFlags) parse() ([]template.Set, error) {
	out := make([]template.Set, 0, len(s.raw))
	for _, r := range s.raw {
		flag := "set"
		if r.asString {
			flag = "set-string"
		}
		name, value, err := pair(flag, r.arg)
		if err != nil {
			return nil, err
		}

		var v any = value
		if !r.asString {
			v = template.ParseValue(value)
		}
		out = append(out, template.Set{Key: name, Value: v})
	}
	return out, nil
}

// renderError classifies what rendering had to say. A path that does not fit the
// body and a parameter nobody supplied are both things the user typed, so both
// are exit 2 — the API never heard about either.
func renderError(err error) error {
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return err
	}
	return exitcode.UsageError{Err: err}
}

// templateName is the template to act on: the argument, or the answer to a
// question when there is a terminal to ask on.
//
// The picker is a numbered list on stderr rather than a full-screen menu. fft has
// no TUI and should not grow one for this: stdout stays the document, and a
// pipeline or an agent — which cannot answer a prompt anyway — gets a usage error
// naming the argument instead of a hang.
func templateName(deps *Deps, store *template.Store, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}

	if !deps.Prompt.Interactive() {
		return "", exitcode.UsageError{Err: fmt.Errorf(
			"%w: name the template to render", prompt.ErrNotInteractive)}
	}

	listing, err := store.List()
	if err != nil {
		return "", err
	}
	reportTemplateProblems(deps, listing.Problems)

	if len(listing.Found) == 0 {
		return "", exitcode.UsageError{Err: fmt.Errorf(
			"there are no templates yet: run 'fft template save <name> --file body.json' to add one")}
	}

	deps.Printer.Notef("Templates:")
	for i, saved := range listing.Found {
		deps.Printer.Notef("  %d) %s  %s", i+1, saved.Name,
			field(deps.Printer.Style(), firstNonEmpty(saved.Description, saved.OperationID)))
	}

	answer, err := deps.Prompt.Validated("Which one", func(s string) error {
		return validChoice(s, len(listing.Found))
	})
	if err != nil {
		return "", err
	}

	index := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(answer), "%d", &index); err != nil {
		return "", exitcode.UsageError{Err: err}
	}
	return listing.Found[index-1].Name, nil
}

func validChoice(s string, n int) error {
	var i int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &i); err != nil {
		return fmt.Errorf("type the number of a template, 1 to %d", n)
	}
	if i < 1 || i > n {
		return fmt.Errorf("there is no template %d: pick one from 1 to %d", i, n)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
