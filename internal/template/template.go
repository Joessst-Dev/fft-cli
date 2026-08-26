// Package template stores request bodies a user sends often, and the parameters
// that vary between sends.
//
// A template is a file, not a row in a database. That is the whole design: the
// bodies people want to keep are the ones they want to review, diff and commit
// next to the code that provokes them, and a store you cannot read with `cat` is
// a store nobody trusts with a payload that changes stock levels.
//
// Nothing here reaches the tenant. A template is rendered to bytes and handed to
// an existing command's --file, so every validation, every read-only refusal and
// every exit code stays exactly where it already was.
package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// Version is the schema version written into every template file.
//
// It exists for the same reason [config.Version] does: a file written by a newer
// fft must be refused with an explanation rather than read as though the fields
// it does not recognise were absent.
const Version = 1

// Param is one parameter a template declares: a name for a path inside the body,
// so that the common edit is `--set email=…` rather than
// `--set order.consumer.email=…`.
//
// Declaring a parameter is optional. --set always accepts a bare path, so a
// template with no params block is fully usable; what a declaration buys is a
// short name, a default, and a required check that fires before the body is ever
// sent anywhere.
type Param struct {
	// Path is where in the body the value goes, in the --set path grammar.
	Path string `json:"path"`

	// Required refuses to render the template until the parameter is given.
	Required bool `json:"required,omitempty"`

	// Default is applied before any --set, so a --set for the same parameter
	// overrides it.
	Default any `json:"default,omitempty"`

	// Description is what `fft template show` prints next to the name.
	Description string `json:"description,omitempty"`
}

// Template is one saved request body and everything fft knows about it.
//
// The name is deliberately not a field: it is the filename, and two places to
// look up one fact is one place too many.
type Template struct {
	SchemaVersion int `json:"schemaVersion"`

	Description string `json:"description,omitempty"`

	// OperationID is the operation the body was written for. It is advisory —
	// rendering never requires the operation to still exist, because a
	// regenerated spec must not strand every template a user saved.
	OperationID string `json:"operationId,omitempty"`

	// Project is the project the template was saved under, so that rendering it
	// against a different tenant can say so. Ids in a body rarely survive the
	// move, and sending them silently is the outcome worth preventing.
	Project string `json:"project,omitempty"`

	Params map[string]Param `json:"params,omitempty"`

	// Body is the request body, decoded with UseNumber so that a 64-bit id or a
	// version survives the round trip as the digits the API sent.
	Body any `json:"body"`
}

// Decode reads a template file.
func Decode(data []byte) (*Template, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var t Template
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("read the template: %w", err)
	}

	if t.SchemaVersion > Version {
		return nil, fmt.Errorf(
			"the template is schema version %d and this fft understands %d: upgrade fft to read it",
			t.SchemaVersion, Version)
	}
	if t.Body == nil {
		return nil, fmt.Errorf("the template has no %q", "body")
	}
	return &t, nil
}

// Encode renders a template for the disk: indented, key-sorted and newline
// terminated, so that two saves of the same body produce the same file and a
// project-local template diffs like the source it sits next to.
func Encode(t *Template) ([]byte, error) {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("write the template: %w", err)
	}
	return append(data, '\n'), nil
}

// Render applies the declared defaults and then the given overrides, and returns
// the finished body as JSON.
//
// Everything that can fail is resolved before the first write lands: a mistyped
// path or a missing required parameter fails against an untouched body, so there
// is never a half-applied render for a caller to wonder about.
func Render(t *Template, sets []Set) ([]byte, error) {
	type override struct {
		path  Path
		value any
	}

	applied := make([]override, 0, len(sets))
	given := make(map[string]bool, len(sets))
	for _, s := range sets {
		path, err := t.resolve(s.Key)
		if err != nil {
			return nil, err
		}
		applied = append(applied, override{path: path, value: s.Value})
		given[path.String()] = true
	}

	missing, err := Missing(t, given)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return nil, &MissingParamsError{Names: missing}
	}

	body, err := clone(t.Body)
	if err != nil {
		return nil, err
	}

	// Defaults first, and only where the user said nothing: a default is what the
	// parameter is when nobody chose, so anything chosen wins.
	for _, name := range sortedParams(t) {
		p := t.Params[name]
		if p.Default == nil {
			continue
		}
		path, err := ParsePath(p.Path)
		if err != nil {
			return nil, fmt.Errorf("parameter %q declares an unusable path: %w", name, err)
		}
		if given[path.String()] {
			continue
		}
		if body, err = Apply(body, path, p.Default); err != nil {
			return nil, fmt.Errorf("apply the default for %q: %w", name, err)
		}
	}

	// Then the overrides, in the order they were typed. The last --set for one
	// path wins, which is what a caller assembling a command line in a loop means.
	for _, o := range applied {
		if body, err = Apply(body, o.path, o.value); err != nil {
			return nil, err
		}
	}

	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render the body: %w", err)
	}
	return append(data, '\n'), nil
}

// resolve turns a --set key into a path: a declared parameter's name means that
// parameter's path, and anything else is a path already.
//
// A declared name can never contain a dot and can never collide with a top-level
// key of the body — both refused when the template is saved — so this lookup has
// exactly one answer and needs no precedence rule at the point of use.
func (t *Template) resolve(key string) (Path, error) {
	if p, ok := t.Params[key]; ok {
		path, err := ParsePath(p.Path)
		if err != nil {
			return nil, fmt.Errorf("parameter %q declares an unusable path: %w", key, err)
		}
		return path, nil
	}
	return ParsePath(key)
}

// Missing lists the required parameters nobody supplied.
//
// given is keyed by canonical path, not by the word the user typed, so setting a
// required parameter by its full path satisfies it exactly as its short name
// would — the two are the same place in the document.
//
// It reports all of them at once. Failing on the first turns filling in a
// four-parameter template into four runs, each telling the user one more thing
// they could already have been told.
func Missing(t *Template, given map[string]bool) ([]string, error) {
	var out []string
	for _, name := range sortedParams(t) {
		p := t.Params[name]
		if !p.Required || p.Default != nil {
			continue
		}
		path, err := ParsePath(p.Path)
		if err != nil {
			return nil, fmt.Errorf("parameter %q declares an unusable path: %w", name, err)
		}
		if !given[path.String()] {
			out = append(out, name)
		}
	}
	return out, nil
}

// sortedParams names the declared parameters in a stable order, so that two runs
// of the same command produce the same output and the same error message.
func sortedParams(t *Template) []string {
	names := make([]string, 0, len(t.Params))
	for name := range t.Params {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// MissingParamsError is a template that cannot be rendered until the user says
// more. It is a usage problem, and its hint is the command line to type.
type MissingParamsError struct {
	Names []string
}

// ExitCode is the usage code, declared here rather than by wrapping the error in
// an exitcode.UsageError: that wrapper wins over the hint when fft renders the
// failure, and the hint — the --set line to type — is the whole of the answer.
func (e *MissingParamsError) ExitCode() int { return exitcode.Usage }

func (e *MissingParamsError) Error() string {
	noun := "parameter"
	if len(e.Names) > 1 {
		noun = "parameters"
	}
	return fmt.Sprintf("the template needs %s %s", noun, quoteList(e.Names))
}

// Hint is the --set line that satisfies the error.
func (e *MissingParamsError) Hint() string {
	var b strings.Builder
	b.WriteString("Add")
	for _, n := range e.Names {
		fmt.Fprintf(&b, " --set %s=<value>", n)
	}
	return b.String() + "."
}

func quoteList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	switch len(quoted) {
	case 1:
		return quoted[0]
	case 2:
		return quoted[0] + " and " + quoted[1]
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	}
}

// clone deep-copies the body so that rendering twice from one loaded template
// cannot have the first render's --set leak into the second.
func clone(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("copy the body: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("copy the body: %w", err)
	}
	return out, nil
}
