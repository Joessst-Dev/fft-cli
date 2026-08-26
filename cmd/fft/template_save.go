package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/template"
)

const templateSaveLong = `Save a request body so you can send it again.

The body comes from --file, --data or --from:

    fft template save rush --file body.json
    fft order get ORDER-1 -o json | fft template save rush --file -
    fft template save rush --from createOrder        seeds from the spec's example

--param declares a parameter: a short name for a path inside the body, so that
'--set email=…' works instead of '--set order.consumer.email=…'. Give it a default
with a second '=', and mark it required with --require:

    --param qty=order.items.0.quantity=1
    --require email=order.consumer.email

A top-level "version" is dropped on the way in. A version is only true of the entity
at the moment it was read, and replaying a stale one is a guaranteed 409 — a saved
body that still carries one is a trap for whoever finds it next. Put one back at
render time with --set version=N if you really want that.

--local writes ./.fft/templates instead of your own directory. That file is meant to
be committed, so read it first: a body captured from real work carries real facility
ids, order ids and consumer emails, and git history is not something you can quietly
edit later.`

func newTemplateSaveCmd(deps *Deps) *cobra.Command {
	var (
		scope       scopeFlag
		file        string
		data        string
		from        string
		description string
		params      []string
		required    []string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "save <name>",
		Short: "Save a request body as a template",
		Long:  templateSaveLong,
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if err := template.ValidateName(name); err != nil {
				return err
			}

			store, err := templateStore()
			if err != nil {
				return err
			}

			// Before the body is read, so that refusing an existing name does not
			// first consume stdin and report what it would have changed.
			if !force {
				exists, err := store.Exists(name, scope.scope())
				if err != nil {
					return err
				}
				if exists {
					return exitcode.UsageError{Err: fmt.Errorf(
						"there is already a %s template named %q: pass --force to replace it",
						scope.scope(), name)}
				}
			}

			raw, operationID, err := templateSource(deps, file, data, from)
			if err != nil {
				return err
			}

			body, err := decodeDoc(raw, "the request body")
			if err != nil {
				return err
			}
			body = unwrapShownTemplate(body)

			declared, err := declaredParams(body, params, required)
			if err != nil {
				return err
			}

			if _, carried := body["version"]; carried {
				delete(body, "version")
				deps.Printer.Notef(
					"Dropped the top-level \"version\": replaying a saved version is a guaranteed 409. " +
						"A handful of fulfillmenttools schemas require it instead; re-supply it with " +
						"--set version=N at render time if this template fails to send with a missing-field error.")
			}

			if scope.scope() == template.ScopeProject {
				if paths := credentialLikePaths(body); len(paths) > 0 {
					ok, err := confirmDestructive(deps, fmt.Sprintf(
						"%s looks like it may carry a credential in %s. Save it to the committed project scope anyway?",
						name, quotedNames(paths)))
					if err != nil {
						return err
					}
					if !ok {
						return exitcode.UsageError{Err: fmt.Errorf("cancelled")}
					}
				}
			}

			path, err := store.Write(name, scope.scope(), &template.Template{
				SchemaVersion: template.Version,
				Description:   description,
				OperationID:   operationID,
				Project:       activeProjectName(deps),
				Params:        declared,
				Body:          body,
			})
			if err != nil {
				return err
			}

			if scope.scope() == template.ScopeProject {
				deps.Printer.Warnf("%s is meant to be committed. Read it before you do: "+
					"facility ids, order ids and consumer emails in git history cannot be quietly taken back.",
					path)
			}
			deps.Printer.Notef("Saved %s. Render it with 'fft template render %s'.", name, name)

			view := templatePathView{Template: name, Path: path}
			return deps.Printer.Render(output.Rows{
				Headers: []string{"TEMPLATE", "PATH"},
				Rows:    [][]string{{view.Template, view.Path}},
			}, view)
		},
	}

	f := cmd.Flags()
	scope.register(cmd, "Write to")
	f.StringVar(&file, "file", "", "JSON file holding the request body ('-' for stdin)")
	f.StringVar(&data, "data", "", "Request body: inline JSON, @file, or '-' for stdin")
	f.StringVar(&from, "from", "", "Seed the body from this operation's example")
	f.StringVar(&description, "description", "", "What this template is for")
	f.StringArrayVar(&params, "param", nil,
		"Declare a parameter: --param name=path[=default] (repeatable)")
	f.StringArrayVar(&required, "require", nil,
		"Declare a required parameter: --require name=path (repeatable)")
	f.BoolVar(&force, "force", false, "Replace a template of the same name")

	cmd.MarkFlagsMutuallyExclusive("file", "data")
	cmd.MarkFlagsMutuallyExclusive("file", "from")
	cmd.MarkFlagsMutuallyExclusive("data", "from")

	return cmd
}

// templatePathView is what save prints. Where the file landed is the answer to
// the command, so it goes on stdout rather than into a sentence on stderr — the
// same argument `fft skill install` makes about its directory.
type templatePathView struct {
	Template string `json:"template" yaml:"template"`
	Path     string `json:"path" yaml:"path"`
}

// templateSource reads the body and works out which operation it belongs to.
//
// --from is both a body and an operationId; --file and --data are a body alone,
// and leave the operation unrecorded unless the user is saving something they
// already know the shape of.
func templateSource(deps *Deps, file, data, from string) ([]byte, string, error) {
	if from != "" {
		op, err := findOperation(from)
		if err != nil {
			return nil, "", err
		}
		if op.SampleBody == "" {
			return nil, "", exitcode.UsageError{Err: fmt.Errorf(
				"the spec has no example body for %s: save one with --file instead", op.ID)}
		}
		return []byte(op.SampleBody), op.ID, nil
	}

	raw, err := requestBody(deps, file, data)
	if err != nil {
		return nil, "", err
	}
	if len(raw) == 0 {
		return nil, "", exitcode.UsageError{Err: fmt.Errorf(
			"a template needs a body: pass --file, --data or --from")}
	}
	return raw, "", nil
}

// declaredParams builds the params block from --param and --require.
//
// Every path is parsed here rather than at render time: a path that will never
// resolve is a mistake worth catching once, now, instead of on every future
// render by somebody who did not write it.
func declaredParams(body entityDoc, params, required []string) (map[string]template.Param, error) {
	out := make(map[string]template.Param)

	// A path never contains '=', so the first one after the name separates the
	// path from a default. --require takes no default: a parameter that is both
	// required and defaulted is a contradiction the user should not be able to
	// express by accident.
	add := func(flag, arg string, isRequired bool) error {
		name, rest, err := pair(flag, arg)
		if err != nil {
			return err
		}
		path, def, hasDefault := strings.Cut(rest, "=")
		if err := validateParamName(name, path, body); err != nil {
			return err
		}
		if isRequired && hasDefault {
			return exitcode.UsageError{Err: fmt.Errorf(
				"--require takes name=path and no default, but %q carries one", arg)}
		}

		p := template.Param{Path: path, Required: isRequired}
		if hasDefault {
			p.Default = template.ParseValue(def)
		}
		if _, err := template.ParsePath(p.Path); err != nil {
			return exitcode.UsageError{Err: fmt.Errorf("--%s %s: %w", flag, arg, err)}
		}
		if _, clash := out[name]; clash {
			return exitcode.UsageError{Err: fmt.Errorf("parameter %q is declared twice", name)}
		}

		out[name] = p
		return nil
	}

	for _, arg := range params {
		if err := add("param", arg, false); err != nil {
			return nil, err
		}
	}
	for _, arg := range required {
		if err := add("require", arg, true); err != nil {
			return nil, err
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// unwrapShownTemplate recognises the envelope `fft template show -o json`
// prints and takes the body back out of it.
//
// templateShowLong promises that `show -o json` round-trips through
// `save --file -`. Without this, that pipe would nest the whole shown
// document — schemaVersion, params, the original body and all — one level
// deeper as the new template's body, which is not a round trip at all. The
// identity check is name-shaped rather than a flag, because a template file
// carries nothing that says "I was printed by show": schemaVersion plus a body
// is what any template file looks like, on disk or piped from show alike, and
// a real fulfillmenttools request body has no plausible reason to declare a
// top-level "schemaVersion" of its own.
func unwrapShownTemplate(body entityDoc) entityDoc {
	sv, ok := body["schemaVersion"].(json.Number)
	if !ok {
		return body
	}
	if n, err := sv.Int64(); err != nil || n <= 0 || n > template.Version {
		return body
	}
	inner, ok := body["body"].(map[string]any)
	if !ok {
		return body
	}
	return inner
}

// credentialFieldPatterns are key-name substrings, matched case-insensitively,
// that a real fulfillmenttools credential-shaped field carries — a password on
// user creation, a clientSecret or firebaseWebApiKey on SSO/OIDC config, a
// bearer token, an Authorization header value.
var credentialFieldPatterns = []string{"password", "secret", "apikey", "token", "authorization"}

// credentialLikePaths lists the dotted paths in body whose key looks like it
// might hold a credential, so save --local can ask before writing one into a
// file whose whole purpose is `git add`.
//
// It is a name-shaped heuristic, not a value-shaped one: fft cannot tell a real
// secret from a placeholder, and the point is to put the file in front of the
// person about to commit it, not to block them.
func credentialLikePaths(body entityDoc) []string {
	var out []string

	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch n := node.(type) {
		case map[string]any:
			for key, v := range n {
				sub := key
				if path != "" {
					sub = path + "." + key
				}
				lower := strings.ToLower(key)
				for _, pattern := range credentialFieldPatterns {
					if strings.Contains(lower, pattern) {
						out = append(out, sub)
						break
					}
				}
				walk(v, sub)
			}
		case []any:
			for i, v := range n {
				walk(v, fmt.Sprintf("%s.%d", path, i))
			}
		}
	}
	walk(map[string]any(body), "")

	slices.Sort(out)
	return out
}

// validateParamName keeps the one namespace --set reads unambiguous.
//
// A name with a dot in it would be indistinguishable from a path, so it is
// refused outright. A name that is also a top-level key of the body is refused
// only when it points somewhere *else* — `--require name=name` is the obvious
// thing to type for a top-level field, and it is not ambiguous at all, because
// both readings of `--set name=x` land in the same place. What is ambiguous is a
// parameter called "name" whose path is "order.consumer.name", next to a body
// that also has a top-level "name": there `--set name=x` would silently mean the
// one the user was not looking at.
//
// Both checks run at save time, so that --set never needs a precedence rule for
// a user to remember at the point of use.
func validateParamName(name, path string, body entityDoc) error {
	if name == "" {
		return exitcode.UsageError{Err: fmt.Errorf("a parameter needs a name")}
	}
	for _, r := range name {
		if r == '.' || r == '\\' {
			return exitcode.UsageError{Err: fmt.Errorf(
				"a parameter name cannot contain %q, because that is what makes it a path and not a name",
				string(r))}
		}
	}
	if _, clash := body[name]; clash && name != path {
		return exitcode.UsageError{Err: fmt.Errorf(
			"%q is already a top-level field of the body, so a parameter of that name pointing at %q "+
				"would make --set %s= mean two different places",
			name, path, name)}
	}
	return nil
}
