package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// Tier 2: a command for every operation nobody has curated.
//
// # Shadowing
//
// A curated command declares the operation it calls in its Annotations, and an
// operation a curated command has claimed gets **no** generated twin. So
// `fft facility list` is the hand-written command, always — and promoting an
// endpoint from Tier 2 to Tier 1 is a pure upgrade rather than a breaking rename,
// because the generated command it replaces disappears the moment the curated one
// declares the same operationId.
//
// The generated commands still land *inside* the curated group: `fft facility` ends
// up holding both the eight hand-written commands and the facility operations
// nobody has curated. One noun, everything under it.
//
// # Grouping
//
// The group is the operation's first tag, singularised: "Facilities (Core)" →
// facility, "Picking (Operations)" → picking. The command within it is the
// operationId, kebab-cased: getPickJob → `fft picking get-pick-job`. That is
// verbose, and it is the point — an operationId is unique across the whole spec, so
// no two generated commands can ever collide, and the short spelling
// (`fft api getPickJob`) is one command away.

// The cobra groups the root command's help is organised into. Without them, 60
// generated groups and 6 hand-written commands would be one undifferentiated list.
const (
	groupCore     = "core"
	groupResource = "resource"
)

// reservedFlags are the flag names a generated command must not derive, because
// something else already owns them.
//
// It is *read off the root command* rather than listed here, and that is the point.
// A hand-copied list would go stale the day someone adds a persistent flag, and the
// failure would not be loud: pflag panics only on a redefine within one FlagSet, and
// cobra merges the root's persistent flags with AddFlagSet, which silently **skips**
// a name the local set already has. So a duplicated `--region` would not crash — the
// generated parameter flag would quietly win, and the global one would never be set.
// A silently shadowed global flag is worse than a panic.
//
// Only the four names cobra and fft add per-command are spelled out, because they
// exist on no FlagSet at the time this runs.
func reservedFlags(root *cobra.Command) map[string]bool {
	taken := map[string]bool{
		"help": true, "file": true, "data": true, "example": true,
	}
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) { taken[f.Name] = true })
	root.Flags().VisitAll(func(f *pflag.Flag) { taken[f.Name] = true })

	return taken
}

// addGeneratedCommands registers a command for every operation the curated tree has
// not claimed. It must run *after* the curated commands are added, because what it
// registers depends on what they claimed.
func addGeneratedCommands(deps *Deps, root *cobra.Command) {
	claimed := claimedOperations(root)
	reserved := reservedFlags(root)

	byGroup := make(map[string][]api.Operation)
	for _, op := range api.Operations() {
		if claimed[op.ID] {
			continue
		}
		byGroup[groupFor(op)] = append(byGroup[groupFor(op)], op)
	}

	groups := make([]string, 0, len(byGroup))
	for name := range byGroup {
		groups = append(groups, name)
	}
	sort.Strings(groups)

	for _, name := range groups {
		parent := resourceGroup(root, name)

		for _, op := range byGroup[name] {
			// An operationId is unique, so its kebab form is too — but a curated command
			// could still have taken the name for itself, and the curated one wins.
			if child, _, err := parent.Find([]string{commandName(op)}); err == nil && child != parent {
				continue
			}
			parent.AddCommand(newGeneratedCmd(deps, op, reserved))
		}
	}
}

// claimedOperations collects the operationIds the curated commands have declared.
func claimedOperations(root *cobra.Command) map[string]bool {
	claimed := make(map[string]bool)

	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if id := cmd.Annotations[annotationOperationID]; id != "" {
			claimed[id] = true
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)

	return claimed
}

// resourceGroup returns the command generated operations of this group hang off,
// reusing the curated one where there is one — which is what puts `fft facility
// get-all-facilities` next to the hand-written `fft facility list`.
func resourceGroup(root *cobra.Command, name string) *cobra.Command {
	for _, child := range root.Commands() {
		if child.Name() == name {
			return child
		}
	}

	group := &cobra.Command{
		Use:     name,
		Short:   fmt.Sprintf("%s operations", name),
		GroupID: groupResource,
		Long: fmt.Sprintf(`%s operations of the fulfillmenttools API.

Every command here is generated from the OpenAPI spec: its flags are the
endpoint's parameters, its --help is the endpoint's own documentation, and — for a
command that takes a body — --example prints one you can edit and send.

  fft %s <command> --help
  fft %s <command> --example > body.json`, name, name, name),

		// A group is a namespace, not a command. Running it prints its commands.
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Args: usageArgs(cobra.NoArgs),
	}
	root.AddCommand(group)

	return group
}

// groupFor is the command group an operation lands in: its first tag, stripped of
// the parenthetical and singularised. "Facilities (Core)" → facility.
func groupFor(op api.Operation) string {
	tag := op.Tag()
	if tag == "" {
		return "other"
	}

	if i := strings.Index(tag, "("); i >= 0 {
		tag = tag[:i]
	}

	name := kebab(strings.TrimSpace(tag))
	if name == "" {
		return "other"
	}
	return singular(name)
}

// commandName is an operation's command: its operationId, kebab-cased.
func commandName(op api.Operation) string { return kebab(op.ID) }

// commandPath is how a user would invoke an operation — the curated command where
// there is one, the generated command otherwise.
//
// It is resolved from the tree rather than computed, so it cannot drift from what
// is actually registered. "" means the operation has no command at all, which
// should not happen and is not worth crashing over.
func commandPath(root *cobra.Command, op api.Operation) string {
	if root == nil {
		return ""
	}

	var found string

	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if found != "" {
			return
		}
		if cmd.Annotations[annotationOperationID] == op.ID {
			found = cmd.CommandPath()
			return
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)

	return found
}

// singular drops the plural from a group name's last word: facilities → facility,
// processes → process, stocks → stock. Only the last word, so
// "carriers-configuration" keeps its carriers.
func singular(name string) string {
	head, last := "", name
	if i := strings.LastIndex(name, "-"); i >= 0 {
		head, last = name[:i+1], name[i+1:]
	}

	switch {
	case len(last) > 3 && strings.HasSuffix(last, "ies"):
		return head + last[:len(last)-3] + "y"

	case strings.HasSuffix(last, "sses"), strings.HasSuffix(last, "shes"),
		strings.HasSuffix(last, "ches"), strings.HasSuffix(last, "xes"),
		strings.HasSuffix(last, "zes"):
		return head + last[:len(last)-2]

	// A word that is not a plural, or one whose singular is itself: status, analysis.
	case !strings.HasSuffix(last, "s"),
		strings.HasSuffix(last, "ss"), strings.HasSuffix(last, "us"), strings.HasSuffix(last, "is"):
		return head + last

	default:
		return head + last[:len(last)-1]
	}
}

// newGeneratedCmd builds the command for one operation: a flag per parameter, and
// the body flags where the operation takes a body.
func newGeneratedCmd(deps *Deps, op api.Operation, reserved map[string]bool) *cobra.Command {
	var (
		file    string
		data    string
		example bool
	)

	cmd := &cobra.Command{
		Use:   commandName(op),
		Short: shortOf(op),
		Args:  usageArgs(cobra.NoArgs),

		// The annotation is what --help reads the endpoint's purpose, permissions and
		// sample body out of, and what tells a curated command from a generated one.
		Annotations: map[string]string{
			annotationOperationID: op.ID,
			annotationGenerated:   "true",
		},
	}

	flags := registerParamFlags(cmd, op, reserved)

	if op.HasBody {
		f := cmd.Flags()
		f.StringVar(&file, "file", "", "JSON file holding the request body ('-' for stdin)")
		f.StringVar(&data, "data", "", "Request body: inline JSON, @file, or '-' for stdin")
		f.BoolVar(&example, "example", false, "Print a sample request body and exit")

		cmd.MarkFlagsMutuallyExclusive("file", "data")
		cmd.MarkFlagsMutuallyExclusive("file", "example")
		cmd.MarkFlagsMutuallyExclusive("data", "example")
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		// --example is answered before anything else: it needs no project, no
		// credentials and no network, and the user reaching for it usually has none of
		// the three set up yet.
		if example {
			return printExample(cmd, op)
		}

		// The deprecation notice is fft's, not cobra's, and that is deliberate: cobra
		// prints its Deprecated field with c.Printf, which goes to **stdout**. For the
		// 21 deprecated operations in the spec that would put an English sentence at the
		// top of `-o json`, and break the pipe into jq for exactly those commands — the
		// kind of bug that ships because nobody runs the deprecated ones.
		if op.Deprecated {
			deps.Printer.Warnf("%s is deprecated: the API may remove it.", op.ID)
		}

		body, err := requestBody(deps, file, data)
		if err != nil {
			return err
		}

		in := opInput{
			Path:   make(map[string]string),
			Query:  make(map[string][]string),
			Header: make(map[string]string),
			Body:   body,
		}

		for _, f := range flags {
			values := f.values(cmd.Flags())
			if len(values) == 0 {
				continue
			}

			switch f.param.In {
			case api.InPath:
				in.Path[f.param.Name] = values[0]

			case api.InHeader:
				// The spec declares no array header today. If it ever does, the extra values
				// must not vanish into a map[string]string without a word — that is the
				// silent-drop class again, and a header fft cannot send is one it must
				// refuse to send.
				if len(values) > 1 {
					return exitcode.UsageError{Err: fmt.Errorf(
						"--%s takes one value, not %d: fft cannot send a repeated %s header",
						f.name, len(values), f.param.Name)}
				}
				in.Header[f.param.Name] = values[0]

			case api.InQuery:
				in.Query[f.param.Name] = values
			}
		}

		// Everything is validated before the client is built, so a missing required
		// parameter exits 2 without signing in and without sending a request.
		req, err := buildRequest(op, in, deps.Printer.Warnf)
		if err != nil {
			return err
		}
		return runOperation(cmd, deps, op, req)
	}

	return cmd
}

// shortOf is the one-line description in the parent's command list. A deprecated
// operation says so here, because this is the line a user reads when choosing.
func shortOf(op api.Operation) string {
	summary := op.Summary
	if summary == "" {
		summary = op.Method + " " + op.Path
	}
	if op.Deprecated {
		return "(deprecated) " + summary
	}
	return summary
}

// opFlag binds a spec parameter to the cobra flag that carries it.
type opFlag struct {
	param api.Param
	name  string

	// One of these holds the value, according to the parameter's type. They are kept
	// apart rather than folded into a string so that --help says "int" where the API
	// wants an int, and so that a value pflag would refuse is refused by pflag.
	str  string
	strs []string
	num  int64
	dec  float64
	yes  bool
}

// values is what the user gave, or nil when they gave nothing.
//
// The question is asked of pflag's Changed and never of the value, because every
// zero value here is something a user might legitimately mean. `--size 0` is a
// request for zero results and `--anonymized false` is a filter; reading either as
// "not given" is how a flag comes to silently do nothing.
func (f *opFlag) values(fs *pflag.FlagSet) []string {
	if !fs.Changed(f.name) {
		return nil
	}

	switch f.param.Type {
	case api.TypeArray:
		return f.strs
	case api.TypeBoolean:
		return []string{strconv.FormatBool(f.yes)}
	case api.TypeInteger:
		return []string{strconv.FormatInt(f.num, 10)}
	case api.TypeNumber:
		return []string{strconv.FormatFloat(f.dec, 'f', -1, 64)}
	default:
		return []string{f.str}
	}
}

// registerParamFlags gives the command a flag per spec parameter. reserved is the
// set of names something else already owns; see [reservedFlags].
func registerParamFlags(cmd *cobra.Command, op api.Operation, reserved map[string]bool) []*opFlag {
	taken := make(map[string]bool, len(reserved)+len(op.Params))
	for name := range reserved {
		taken[name] = true
	}

	out := make([]*opFlag, 0, len(op.Params))
	fs := cmd.Flags()

	for _, p := range op.Params {
		f := &opFlag{param: p, name: flagName(p, taken)}
		taken[f.name] = true

		usage := flagUsage(p)

		switch p.Type {
		case api.TypeArray:
			fs.StringArrayVar(&f.strs, f.name, nil, usage)
		case api.TypeBoolean:
			fs.BoolVar(&f.yes, f.name, false, usage)
		case api.TypeInteger:
			fs.Int64Var(&f.num, f.name, 0, usage)
		case api.TypeNumber:
			fs.Float64Var(&f.dec, f.name, 0, usage)
		default:
			fs.StringVar(&f.str, f.name, "", usage)
		}

		if len(p.Enum) > 0 {
			registerEnumCompletion(cmd, f.name, p.Enum)
		}
		out = append(out, f)
	}

	return out
}

// flagName is a parameter's flag, kebab-cased and disambiguated.
//
// Two things can take the name first: a root flag (--timeout, --output) and another
// parameter of the same operation — a name can appear both in the path and in the
// query. Either way the suffix says which parameter this is rather than which one
// it lost to.
func flagName(p api.Param, taken map[string]bool) string {
	base := kebab(p.Name)

	for _, candidate := range []string{base, base + "-" + string(p.In)} {
		if !taken[candidate] {
			return candidate
		}
	}

	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// flagUsage is the flag's one-line help: what the parameter is, and the two things
// about it a user cannot guess — whether it is required, and how a list is encoded.
func flagUsage(p api.Param) string {
	var b strings.Builder

	if p.Description != "" {
		b.WriteString(truncateAt(p.Description, 100))
	} else {
		fmt.Fprintf(&b, "%s parameter %s", p.In, p.Name)
	}

	if len(p.Enum) > 0 {
		fmt.Fprintf(&b, " (one of %s)", strings.Join(p.Enum, ", "))
	}

	if p.Type == api.TypeArray {
		// The encoding of a repeated flag is the one thing about it that is silently
		// wrong when it is wrong, so it is in the flag's own help and not just the
		// command's.
		if p.Explode {
			b.WriteString(" (repeatable; sent as repeated parameters)")
		} else {
			b.WriteString(" (repeatable; sent comma-joined)")
		}
	}

	if p.Required {
		b.WriteString(" (required)")
	}
	return b.String()
}
