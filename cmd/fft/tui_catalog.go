package main

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// untaggedGroup is the group an operation the spec gives no tag is listed under.
const untaggedGroup = "Other"

// cobraMutuallyExclusive is the flag annotation cobra records
// MarkFlagsMutuallyExclusive in. cobra does not export the name; the catalog spec
// pins it against a real command.
const cobraMutuallyExclusive = "cobra_annotation_mutually_exclusive"

// formFlagsExcluded are the flags the request form never offers, beyond the global
// ones: the body travels on stdin through --file, and --data and --example are
// other ways of giving or asking for it.
var formFlagsExcluded = []string{"help", "file", "data", "example"}

// cliCatalog is the TUI's [tui.Catalog]: every operation, and the command in the
// real tree that sends it. It is computed once, from a tree that is never executed,
// and is immutable afterwards.
type cliCatalog struct {
	groups []tui.OperationGroup
}

var _ tui.Catalog = (*cliCatalog)(nil)

// newCLICatalog reads the catalog off root. cobra builds a command's merged flag
// set lazily, the first time it is asked for, so this must run before anything
// else touches root from another goroutine.
func newCLICatalog(root *cobra.Command) *cliCatalog {
	commands := operationCommands(root)
	global := make(map[string]bool)
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) { global[f.Name] = true })

	viaEscape := escapeCommand(root, global)

	byTag := make(map[string][]tui.Operation)
	for _, op := range api.Operations() {
		tag := op.Tag()
		if tag == "" {
			tag = untaggedGroup
		}

		claimants := commands[op.ID]
		sender := operationSender(op, claimants)
		var cmd tui.Command
		switch {
		case sender != nil:
			cmd = describeCommand(sender, op, global)
		case viaEscape != nil:
			// No single command of fft's own stands for it: an installed component
			// claimed it, or several curated commands each send one use of it. `fft
			// api` reaches it, and sends the body as it is.
			cmd = viaEscape(op)
		default:
			continue
		}

		var also []tui.Command
		for _, c := range claimants {
			if c != sender {
				also = append(also, describeCommand(c, op, global))
			}
		}
		if sender != nil && viaEscape != nil {
			also = append(also, viaEscape(op))
		}

		byTag[tag] = append(byTag[tag], tui.Operation{
			ID:          op.ID,
			Summary:     op.Summary,
			Description: op.Description,
			Method:      op.Method,
			Path:        op.Path,
			Tag:         tag,
			Permissions: slices.Clone(op.Permissions),
			Mutates:     op.Mutates(),
			Deprecated:  op.Deprecated,
			SampleBody:  op.SampleBody,
			Command:     cmd,
			Also:        also,
		})
	}

	// First tags only, so that an operation with two is listed once, under the tag
	// its generated command is grouped by. api.OperationsByTag matches every tag by
	// substring, which is right for `fft api list --tag` and would list such an
	// operation twice here.
	tags := slices.Clone(api.Tags())
	if len(byTag[untaggedGroup]) > 0 && !slices.Contains(tags, untaggedGroup) {
		tags = append(tags, untaggedGroup)
	}
	c := &cliCatalog{}
	for _, tag := range tags {
		if ops := byTag[tag]; len(ops) > 0 {
			c.groups = append(c.groups, tui.OperationGroup{Tag: tag, Operations: ops})
		}
	}
	return c
}

// Groups implements [tui.Catalog].
func (c *cliCatalog) Groups() []tui.OperationGroup { return c.groups }

// Table implements [tui.Catalog].
func (c *cliCatalog) Table(cmd tui.Command, stdout []byte) (string, error) {
	return renderTable(cmd.Path, stdout)
}

// escapeCommand returns what the request form needs to know about `fft api <id>`
// for an operation, or nil when root has no `fft api`. Its flags do not depend on
// the operation, so they are read once rather than once for each of the API's.
func escapeCommand(root *cobra.Command, global map[string]bool) func(api.Operation) tui.Command {
	escape, _, err := root.Find([]string{"api"})
	if err != nil || escape == root {
		return nil
	}
	base := describeCommand(escape, api.Operation{}, global)
	return func(op api.Operation) tui.Command {
		cmd := base
		cmd.Path = append(slices.Clone(base.Path), op.ID)
		cmd.Args = nil
		cmd.Flags = slices.Clone(base.Flags)
		cmd.Curated, cmd.Example = false, false
		cmd.Body, cmd.BodyRequired = op.HasBody, op.BodyRequired
		return cmd
	}
}

// operationCommands maps each operationId to the commands that declare it, in the
// order [commandPath] finds them.
func operationCommands(root *cobra.Command) map[string][]*cobra.Command {
	found := make(map[string][]*cobra.Command)
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if id := cmd.Annotations[annotationOperationID]; id != "" {
			found[id] = append(found[id], cmd)
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
	return found
}

// annotationSharedReadSender marks a curated command that may stand for its
// operation in the request form when other commands claim the operation too. Only a
// command that sends the body the user wrote may carry it — checked against the
// schema, sent as written, with nothing of its own but the paging its flags set —
// because the form offers the operation's body, not one command's use of it.
const annotationSharedReadSender = "sharedReadSender"

// operationSender is the command the request form for op runs, out of the commands
// that claim op, or nil when none of them can stand for the operation and it must
// go through `fft api`.
//
// A write several commands share is never given to one of them: `order cancel` and
// `order unlock` are both orderAction, and naming either would make the form send
// that one use, whatever body the user wrote. A shared read with a body goes to the
// one claimant marked [annotationSharedReadSender], which adds the curated table
// and the paging to what `fft api` would send. Anything else, including two marked
// claimants, stays with `fft api`: the catalog spec's census makes a new shared
// operation a decision.
func operationSender(op api.Operation, claimants []*cobra.Command) *cobra.Command {
	switch {
	case len(claimants) == 1:
		return claimants[0]
	case op.Mutates(), !op.HasBody:
		return nil
	}

	var sender *cobra.Command
	for _, c := range claimants {
		if c.Annotations[annotationSharedReadSender] == "" {
			continue
		}
		if sender != nil {
			return nil
		}
		sender = c
	}
	return sender
}

// describeCommand is what the request form needs to know about cmd, which sends op.
func describeCommand(cmd *cobra.Command, op api.Operation, global map[string]bool) tui.Command {
	path := strings.Fields(cmd.CommandPath())[1:]
	args, required := usageSyntax(cmd.Use)
	generated := cmd.Annotations[annotationGenerated] != ""

	out := tui.Command{
		Path:     path,
		Curated:  !generated,
		Args:     args,
		Confirms: cmd.Annotations[annotationConfirms] != "",
		Table:    commandTables[strings.Join(path, " ")] != nil,
	}

	flags := cmd.LocalFlags()
	out.Body = flags.Lookup("file") != nil
	switch {
	case !out.Body:
	case generated:
		out.BodyRequired = op.BodyRequired
	default:
		out.BodyRequired = required["file"]
	}
	// A generated command's --example prints the operation's SampleBody, which the
	// UI already has. A curated one may print a body of its own, written because the
	// schema is wrong about what the API needs.
	out.Example = !generated && flags.Lookup("example") != nil

	add := func(f *pflag.Flag) {
		if f.Hidden || f.Deprecated != "" || global[f.Name] || slices.Contains(formFlagsExcluded, f.Name) {
			return
		}
		out.Flags = append(out.Flags, describeFlag(f, required[f.Name]))
	}
	flags.VisitAll(add)
	cmd.InheritedFlags().VisitAll(add)

	// Required first: they are the ones the form cannot be sent without.
	slices.SortStableFunc(out.Flags, func(a, b tui.Flag) int {
		switch {
		case a.Required == b.Required:
			return strings.Compare(a.Name, b.Name)
		case a.Required:
			return -1
		default:
			return 1
		}
	})
	return out
}

func describeFlag(f *pflag.Flag, requiredByUse bool) tui.Flag {
	kind := flagKind(f.Value.Type())
	if kind == tui.FlagList && len(f.Annotations[flagAnnotationPairs]) > 0 {
		kind = tui.FlagPairs
	}
	out := tui.Flag{
		Name:        f.Name,
		Kind:        kind,
		Usage:       f.Usage,
		Required:    requiredByUse || len(f.Annotations[flagAnnotationRequired]) > 0,
		Enum:        slices.Clone(f.Annotations[flagAnnotationEnum]),
		WithExample: true,
	}
	switch f.DefValue {
	case "", "false", "0", "[]", "0s":
	default:
		out.Default = f.DefValue
	}
	for _, group := range f.Annotations[cobraMutuallyExclusive] {
		if slices.Contains(strings.Fields(group), "example") {
			out.WithExample = false
		}
	}
	return out
}

func flagKind(typ string) tui.FlagKind {
	switch typ {
	case "bool":
		return tui.FlagBool
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "count":
		return tui.FlagInt
	case "float32", "float64":
		return tui.FlagFloat
	case "duration":
		return tui.FlagDuration
	case "stringArray", "stringSlice", "intSlice", "int64Slice", "boolSlice", "float64Slice", "durationSlice":
		return tui.FlagList
	default:
		return tui.FlagString
	}
}

// usageSyntax reads a command's positional arguments, and the flags it cannot run
// without, off its usage line: "get <id> --facility <id>" takes one required
// argument and requires --facility.
//
// The usage line is where the curated commands say so, for the help's sake; they
// check it themselves in RunE, where nothing else can see it. A token in square
// brackets is optional, and "<a>|<b>" alternatives are one argument.
func usageSyntax(use string) (args []tui.Arg, requiredFlags map[string]bool) {
	requiredFlags = make(map[string]bool)
	fields := strings.Fields(use)
	if len(fields) == 0 {
		return nil, requiredFlags
	}

	depth := 0
	for i := 1; i < len(fields); i++ {
		token := fields[i]
		optional := depth > 0 || strings.HasPrefix(token, "[")
		depth += strings.Count(token, "[") - strings.Count(token, "]")

		switch {
		case strings.HasPrefix(strings.TrimLeft(token, "["), "--"):
			name, _, _ := strings.Cut(strings.TrimLeft(token, "[-"), "=")
			name = strings.TrimRight(name, "]")
			if !optional {
				requiredFlags[name] = true
			}
			// The flag's own placeholder is not an argument.
			if i+1 < len(fields) && strings.HasPrefix(fields[i+1], "<") {
				i++
				depth += strings.Count(fields[i], "[") - strings.Count(fields[i], "]")
			}
		case strings.HasPrefix(strings.TrimLeft(token, "["), "<"):
			name := strings.Trim(token, "[]<>.")
			if head, _, found := strings.Cut(name, ">|<"); found {
				name = head
			} else if head, _, found := strings.Cut(name, ">"); found {
				name = head
			}
			args = append(args, tui.Arg{Name: name, Required: !optional})
		}
	}
	return args, requiredFlags
}
