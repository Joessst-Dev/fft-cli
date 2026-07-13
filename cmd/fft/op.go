package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// The machinery shared by the two uncurated tiers.
//
// Tier 3 is `fft api <operationId> --param k=v --query k=v`: one command that can
// call anything. Tier 2 is a command per operation, with real flags derived from
// the spec. They differ only in how the user names a value; from the point where
// the values exist, they are the same code — the same validation, the same
// encoding, the same rendering. That is the point of this file: there is exactly
// one place where a parameter becomes a query string, and it reads `explode` off
// the parameter rather than deciding for itself.

// opInput is what the user gave, sorted into the places the API takes it.
type opInput struct {
	// Path fills the path template's placeholders.
	Path map[string]string

	// Query holds the query parameters. A value may be multi-valued whether or not
	// the parameter is an array; [buildRequest] is what decides.
	Query map[string][]string

	// Header holds the header parameters.
	Header map[string]string

	// Body is the request body, or nil for none.
	Body []byte
}

// buildRequest validates the input against the spec and turns it into a request.
//
// Every check happens here, before a single byte goes over the network. A missing
// required parameter, an enum value the API does not have, a body for an operation
// that takes none — all of them exit 2 with nothing sent, because a request the CLI
// could see was wrong is a request the user should not be billed a round trip for.
//
// warn reports the one thing that is *not* an error: a query parameter the spec
// does not declare. `fft api` is the escape hatch, and an escape hatch that refuses
// to send a parameter the spec forgot is not one — so the parameter is sent, and
// the user is told on stderr that fft did not recognise it. That is the difference
// between "your filter had a typo" (which they will now see) and a filter that
// quietly matched nothing.
func buildRequest(op api.Operation, in opInput, warn func(format string, args ...any)) (client.RawRequest, error) {
	var missing []string

	req := client.RawRequest{
		Method:     op.Method,
		Path:       op.Path,
		PathParams: make(map[string]string, len(in.Path)),
		Header:     make(map[string]string, len(in.Header)),
		Body:       in.Body,
	}

	// Path and header parameters, from the spec's side: a required one with no value
	// is the error, and a value for a parameter the spec has never heard of cannot be
	// placed anywhere at all.
	for _, p := range op.Params {
		switch p.In {
		case api.InPath:
			// Trimmed before it is stored, not only before it is checked: a stray space
			// from a shell variable would otherwise be escaped into the path as %20 and
			// address an id that does not exist.
			value := strings.TrimSpace(in.Path[p.Name])
			if value == "" {
				missing = append(missing, describeParam(p))
				continue
			}

			value, err := checkEnum(p, value)
			if err != nil {
				return client.RawRequest{}, err
			}
			req.PathParams[p.Name] = value

		case api.InHeader:
			value := strings.TrimSpace(in.Header[p.Name])
			if value == "" {
				if p.Required {
					missing = append(missing, describeParam(p))
				}
				continue
			}

			value, err := checkEnum(p, value)
			if err != nil {
				return client.RawRequest{}, err
			}
			req.Header[p.Name] = value

		case api.InQuery:
			if p.Required && len(in.Query[p.Name]) == 0 {
				missing = append(missing, describeParam(p))
			}
		}
	}

	for name := range in.Path {
		if _, ok := specParam(op, api.InPath, name); !ok {
			return client.RawRequest{}, exitcode.UsageError{Err: fmt.Errorf(
				"%s has no path parameter %q: it takes %s",
				op.ID, name, nameList(op, api.InPath))}
		}
	}

	// A header the spec does not declare is still a header: it goes where it was
	// told, because that is what a header is for.
	for name, value := range in.Header {
		if _, ok := specParam(op, api.InHeader, name); !ok {
			req.Header[name] = value
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return client.RawRequest{}, exitcode.UsageError{Err: fmt.Errorf(
			"%s needs %s", op.ID, strings.Join(missing, ", "))}
	}

	query, err := buildQuery(op, in.Query, warn)
	if err != nil {
		return client.RawRequest{}, err
	}
	req.Query = query

	if err := checkBody(op, in.Body); err != nil {
		return client.RawRequest{}, err
	}
	return req, nil
}

// buildQuery encodes the query string, taking each parameter's `explode` from its
// own entry in the spec.
//
// This is the silent-bug line of the whole milestone. `status` is a comma-joined
// array on a pickjob and a repeated one on a handover — the same name, two
// encodings — and the API answers 200 to either. Get it wrong and the user is told
// their filter matched nothing, which is a sentence they will believe.
func buildQuery(op api.Operation, values map[string][]string, warn func(string, ...any)) ([]client.QueryParam, error) {
	// Sorted so that --debug's dump and a spec's assertion see a stable request.
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]client.QueryParam, 0, len(names))

	for _, name := range names {
		given := values[name]
		if len(given) == 0 {
			continue
		}

		p, known := specParam(op, api.InQuery, name)
		if !known {
			if warn != nil {
				warn("%s has no query parameter %q. Sending it anyway; the API may ignore it.", op.ID, name)
			}
			// An undeclared parameter has no `explode` to read, and repeating the name is
			// the OpenAPI default for a query parameter.
			out = append(out, client.QueryParam{Name: name, Values: given, Explode: true})
			continue
		}

		vals := given
		if p.Type == api.TypeArray {
			// --status OPEN,CLOSED and --status OPEN --status CLOSED are the same two
			// values. Splitting here — rather than letting a comma reach the wire as part
			// of one value — is what makes them behave identically under *both* encodings.
			vals = splitList(given)

			// An empty value is refused rather than dropped. `--status ""` — which is
			// what a script writes when its $STATUS is unset — splits to nothing, and a
			// parameter with no values is not sent at all: the API would answer 200 with
			// *every* row, and the user would read an unfiltered list as a filtered one.
			// Too many rows is the harder direction to notice, and this is exactly the
			// silent-wrong-filter class the whole tier exists to prevent.
			if len(vals) == 0 {
				return nil, exitcode.UsageError{Err: fmt.Errorf(
					"--%s was given no value: it is a list, and an empty one would send no filter at all", name)}
			}
		} else if len(given) > 1 {
			return nil, exitcode.UsageError{Err: fmt.Errorf(
				"%s takes one --%s, not %d: it is not a list", op.ID, name, len(given))}
		}

		checked := make([]string, 0, len(vals))
		for _, v := range vals {
			value, err := checkEnum(p, v)
			if err != nil {
				return nil, err
			}
			checked = append(checked, value)
		}

		out = append(out, client.QueryParam{Name: name, Values: checked, Explode: p.Explode})
	}
	return out, nil
}

// checkBody refuses the two body mistakes the API would answer with a vague 400.
func checkBody(op api.Operation, body []byte) error {
	switch {
	case body != nil && !op.HasBody:
		return exitcode.UsageError{Err: fmt.Errorf(
			"%s %s takes no request body", op.Method, op.Path)}

	case body == nil && op.BodyRequired:
		hint := ""
		if op.SampleBody != "" {
			hint = fmt.Sprintf(": run 'fft api %s --example' for a body to start from", op.ID)
		}
		return exitcode.UsageError{Err: fmt.Errorf(
			"%s needs a request body, given with --file or --data%s", op.ID, hint)}

	case body != nil && !json.Valid(body):
		return exitcode.UsageError{Err: fmt.Errorf("the request body is not valid JSON")}
	}
	return nil
}

// checkEnum normalises a value to the API's spelling, or refuses it.
//
// Case is forgiven — `--status open` is what a human types — but a value that is
// not in the enum at all is refused rather than sent. The API would take it, answer
// 200, and match nothing.
func checkEnum(p api.Param, value string) (string, error) {
	if len(p.Enum) == 0 {
		return value, nil
	}

	for _, allowed := range p.Enum {
		if strings.EqualFold(strings.TrimSpace(value), allowed) {
			return allowed, nil
		}
	}
	return "", exitcode.UsageError{Err: fmt.Errorf(
		"unknown %s %q: want one of %s", p.Name, value, strings.Join(p.Enum, ", "))}
}

// splitList flattens comma-separated values into the list they stand for.
func splitList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// specParam finds a parameter by name and location. Name alone is not enough: a
// name can appear both in the path and in the query, and they are two parameters
// with two encodings.
func specParam(op api.Operation, in api.ParamIn, name string) (api.Param, bool) {
	for _, p := range op.Params {
		if p.In == in && p.Name == name {
			return p, true
		}
	}
	return api.Param{}, false
}

// nameList names the parameters of one location, for an error that has to say what
// would have been accepted.
func nameList(op api.Operation, in api.ParamIn) string {
	params := op.ParamsIn(in)
	if len(params) == 0 {
		return "none"
	}

	names := make([]string, 0, len(params))
	for _, p := range params {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// describeParam names a missing parameter the way the user would have supplied it.
func describeParam(p api.Param) string {
	return fmt.Sprintf("%s (%s)", p.Name, p.In)
}

// runOperation sends a built request and renders the answer. It is the last three
// lines of every Tier-2 and Tier-3 command.
func runOperation(cmd *cobra.Command, deps *Deps, op api.Operation, req client.RawRequest) error {
	c, err := tenantClient(deps)
	if err != nil {
		return err
	}

	ctx, cancel := deps.Context(cmd)
	defer cancel()

	res, err := c.DoRaw(ctx, operationName(op), req)
	if err != nil {
		return err
	}
	return renderResult(deps, res)
}

// operationName is how an operation is named in an error: its summary if it has
// one, else its id. "get the pick job: the API returned HTTP 404" reads better than
// an operationId nobody typed.
func operationName(op api.Operation) string {
	if op.Summary != "" {
		return strings.ToLower(op.Summary)
	}
	return op.ID
}

// renderResult prints an operation's answer.
//
// Three things can come back and all three are legitimate. A JSON document is
// rendered. An empty body is a 204 or a mutation that says nothing, and it is
// reported on *stderr* — stdout stays empty, so a pipe receives nothing rather than
// a word. And a body that is not JSON at all — the label and document endpoints
// return PDFs — is written to stdout untouched, so `fft api getPickJobTransferLabel
// ... > label.pdf` produces a PDF and not a mangled one.
func renderResult(deps *Deps, res *client.Result) error {
	if len(res.Body) == 0 {
		deps.Printer.Notef("The API answered HTTP %d with no content.", res.Status)
		return nil
	}

	if !json.Valid(res.Body) {
		if _, err := deps.Printer.Out().Write(res.Body); err != nil {
			return fmt.Errorf("write the API's answer: %w", err)
		}
		return nil
	}
	return deps.Printer.RenderDocument(res.Body)
}

// printCommandExample is --example on a *curated* command: it prints the sample body
// of the operation the command declares in its annotations.
//
// The body is the synthesized one, from the spec — so it does not have to be
// maintained by hand and cannot go stale while the schema moves. Where a
// hand-written body is genuinely better than the synthesized one, the command keeps
// it and does not call this; `fft stock create` is the case, because the spec marks
// the facility selector optional and the API does not, so nothing derived from the
// schema can produce a body that works.
func printCommandExample(cmd *cobra.Command) error {
	op, ok := operationOf(cmd)
	if !ok {
		return fmt.Errorf("%s declares no operation, so there is no example body for it", cmd.CommandPath())
	}
	return printExample(cmd, op)
}

// printExample writes an operation's synthesized sample body to stdout.
//
// It goes to stdout, and it is the command's whole output, because the point of it
// is `fft api addPickJob --example > body.json`.
func printExample(cmd *cobra.Command, op api.Operation) error {
	if op.SampleBody == "" {
		// Asking a GET for its request body is a usage mistake (exit 2), not a
		// failure — `fft api getPickJob --example` reaches this, and every one of the
		// 271 operations that does take a body has one to show.
		return exitcode.UsageError{Err: fmt.Errorf(
			"%s %s takes no request body, so there is no example of one", op.Method, op.Path)}
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), op.SampleBody)
	return err
}

// kebab turns a camelCase spec name into the flag or command name a CLI user
// expects: pickJobId → pick-job-id, getOIDCConfiguration → get-oidc-configuration.
//
// A run of capitals is one word, so the boundary is only taken where a capital
// follows a lower-case letter or precedes one.
func kebab(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)

	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_' || r == ' ' || r == '.' || r == '/':
			// A separator the spec already used; do not double it.
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
			continue

		case unicode.IsUpper(r):
			lowerBefore := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			lowerAfter := i > 0 && i+1 < len(runes) && unicode.IsLower(runes[i+1])

			if (lowerBefore || lowerAfter) && b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))

		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Trim(b.String(), "-")
}
