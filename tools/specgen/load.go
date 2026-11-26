package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// The extension the API declares its required permissions in. 344 of the 576
// operations carry one; it is a plain array of strings at operation level.
const permissionsExtension = "x-fft-permissions"

// operation mirrors api.Operation. It is declared here rather than imported from
// internal/api on purpose: if specgen imported the package it generates into, a
// generated file that does not compile could not be regenerated, and the only way
// out of that would be to hand-edit it.
type operation struct {
	ID             string
	Method         string
	Path           string
	Tags           []string
	Summary        string
	Description    string
	Permissions    []string
	Params         []param
	HasBody        bool
	BodyRequired   bool
	SampleBody     string
	SampleResponse string
	Deprecated     bool
	Paged          bool
	ItemsKey       string
}

type param struct {
	Name        string
	In          string
	Required    bool
	Type        string
	Item        string
	Explode     bool
	Enum        []string
	Min, Max    int
	Description string
}

// load reads the spec and flattens it into the operations table, sorted by
// operationId so the output is a deterministic function of the input.
func load(path string) ([]operation, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false

	doc, err := loader.LoadFromFile(path)
	if err != nil {
		return nil, err
	}
	if doc.Paths == nil {
		return nil, fmt.Errorf("the spec declares no paths")
	}

	syn := &synthesizer{}

	var ops []operation
	seen := make(map[string]string)

	for _, tmpl := range sortedKeys(doc.Paths.Map()) {
		item := doc.Paths.Value(tmpl)

		for _, method := range sortedKeys(item.Operations()) {
			src := item.Operations()[method]
			if src.OperationID == "" {
				return nil, fmt.Errorf("%s %s has no operationId", method, tmpl)
			}
			// An operationId is the name `fft api <id>` takes, so a duplicate would
			// make one of the two unreachable. Fail the build rather than pick.
			if before, dup := seen[src.OperationID]; dup {
				return nil, fmt.Errorf("operationId %q is used by both %s and %s %s",
					src.OperationID, before, method, tmpl)
			}
			seen[src.OperationID] = method + " " + tmpl

			// A path-level parameter applies to every operation on the path, and the
			// operation may override it by name+in (OpenAPI 3.0 §4.7.9.1).
			params, err := loadParams(merge(item.Parameters, src.Parameters))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", src.OperationID, err)
			}

			sample, err := syn.body(src.RequestBody)
			if err != nil {
				return nil, fmt.Errorf("%s: synthesize the request body: %w", src.OperationID, err)
			}

			response, err := syn.response(src.Responses)
			if err != nil {
				return nil, fmt.Errorf("%s: synthesize the response body: %w", src.OperationID, err)
			}

			paged, itemsKey := pagedList(method, params, src.Responses)

			ops = append(ops, operation{
				ID:             src.OperationID,
				Method:         method,
				Path:           tmpl,
				Tags:           append([]string(nil), src.Tags...),
				Summary:        prose(src.Summary),
				Description:    prose(src.Description),
				Permissions:    permissions(src.Extensions),
				Params:         params,
				HasBody:        src.RequestBody != nil,
				BodyRequired:   src.RequestBody != nil && src.RequestBody.Value != nil && src.RequestBody.Value.Required,
				SampleBody:     sample,
				SampleResponse: response,
				Deprecated:     src.Deprecated,
				Paged:          paged,
				ItemsKey:       itemsKey,
			})
		}
	}

	slices.SortFunc(ops, func(a, b operation) int { return cmp.Compare(a.ID, b.ID) })
	return ops, nil
}

// merge overlays an operation's parameters on the path's. A parameter is the same
// one when its name *and* location match — `id` in the path and `id` in the query
// are two parameters.
func merge(pathLevel, opLevel openapi3.Parameters) openapi3.Parameters {
	out := make(openapi3.Parameters, 0, len(pathLevel)+len(opLevel))

	overridden := func(p *openapi3.Parameter) bool {
		for _, o := range opLevel {
			if o.Value != nil && o.Value.Name == p.Name && o.Value.In == p.In {
				return true
			}
		}
		return false
	}

	for _, ref := range pathLevel {
		if ref.Value != nil && !overridden(ref.Value) {
			out = append(out, ref)
		}
	}
	return append(out, opLevel...)
}

// loadParams flattens an operation's parameters, sorted path → query → header and
// alphabetically within each, so that --help lists them in the order a user fills
// them in and so that the generated file does not churn.
func loadParams(refs openapi3.Parameters) ([]param, error) {
	var out []param

	for _, ref := range refs {
		src := ref.Value
		if src == nil {
			return nil, fmt.Errorf("parameter %q has no definition", ref.Ref)
		}

		switch src.In {
		case openapi3.ParameterInPath, openapi3.ParameterInQuery, openapi3.ParameterInHeader:
		default:
			// A cookie parameter is not something fft sends. Dropping it silently would
			// be the worse choice only if the spec had one; it does not.
			continue
		}

		p := param{
			Name:        src.Name,
			In:          src.In,
			Required:    src.Required || src.In == openapi3.ParameterInPath,
			Type:        "string",
			Description: prose(src.Description),
		}

		if s := src.Schema; s != nil && s.Value != nil {
			p.Type = jsonType(s.Value)
			p.Enum = enumStrings(s.Value.Enum)
			p.Min, p.Max = bounds(s.Value)

			if p.Type == "array" && s.Value.Items != nil && s.Value.Items.Value != nil {
				p.Item = jsonType(s.Value.Items.Value)
				if len(p.Enum) == 0 {
					p.Enum = enumStrings(s.Value.Items.Value.Enum)
				}
			}
		}
		p.Explode = explode(src)

		out = append(out, p)
	}

	rank := map[string]int{
		openapi3.ParameterInPath:   0,
		openapi3.ParameterInQuery:  1,
		openapi3.ParameterInHeader: 2,
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].In] != rank[out[j].In] {
			return rank[out[i].In] < rank[out[j].In]
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// explode is the parameter's effective explode: what it says, or what its style
// implies when it says nothing.
//
// This is the whole reason the table exists. `form` — the default style for a
// query parameter — defaults explode to *true*, and 17 of the spec's 77 array
// query parameters override it to false. Applying either answer to all of them
// produces a request the API accepts and answers with the wrong rows: pickjob
// `status` wants status=A,B and facility `status` wants status=A&status=B, and the
// server does not complain about being given the other one, it just filters on
// something else.
func explode(p *openapi3.Parameter) bool {
	if p.Explode != nil {
		return *p.Explode
	}

	switch p.Style {
	case "", "form":
		// form (query) and simple (path/header) are the defaults for their locations;
		// only form defaults to exploding.
		return p.In == openapi3.ParameterInQuery
	case "spaceDelimited", "pipeDelimited", "deepObject":
		return false
	default:
		return false
	}
}

// jsonType names a schema's type the way [api.ParamType] spells it, defaulting to
// string — a parameter the spec gave no type is one fft passes through untouched.
func jsonType(s *openapi3.Schema) string {
	if s.Type == nil {
		return "string"
	}
	for _, t := range []string{"array", "boolean", "integer", "number", "string"} {
		if s.Type.Is(t) {
			return t
		}
	}
	return "string"
}

// enumStrings renders an enum as the strings a flag would accept. A value that is
// not a scalar is not something a user can type, so it is dropped.
func enumStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		switch v := v.(type) {
		case string:
			out = append(out, v)
		case bool, float64, int, int64, json.Number:
			out = append(out, fmt.Sprint(v))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// permissions reads x-fft-permissions. The extension is a plain array of strings,
// but which Go type kin-openapi hands it back as has changed between releases, so
// it is round-tripped through JSON rather than type-asserted.
func permissions(ext map[string]any) []string {
	raw, ok := ext[permissionsExtension]
	if !ok {
		return nil
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}

	var out []string
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}

	sort.Strings(out)
	return out
}

// sortedKeys is the guard on codegen determinism: every walk over a map in this
// program goes through it, because Go randomises map order and CI fails the build
// on a diff.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// prose turns the spec's HTML into something a terminal can print: the descriptions
// carry <a href> links and <br /> tags, which are noise in --help.
func prose(s string) string {
	s = breaks.ReplaceAllString(s, " ")
	s = tags.ReplaceAllString(s, "")
	s = strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&nbsp;", " ",
	).Replace(s)
	return strings.TrimSpace(space.ReplaceAllString(s, " "))
}

// bounds reads a numeric schema's minimum and maximum as the whole numbers a flag
// can be checked against. A bound that is not a whole number is dropped rather than
// rounded: a check fft cannot state exactly would refuse a value the API accepts.
func bounds(s *openapi3.Schema) (lo, hi int) {
	whole := func(f *float64) int {
		if f == nil || *f != float64(int(*f)) {
			return 0
		}
		return int(*f)
	}
	return whole(s.Min), whole(s.Max)
}

// pagedList recognises the GET lists fft can page with startAfterId, and names the
// field their entities are in.
//
// Two response shapes qualify: a bare array (GET /api/categories), and an object
// with exactly one array property plus `total` ({"articles": [...], "total": n}).
// The `total` is required for the envelope because the paginator cross-checks it —
// without it a server that caps `size` would look like the end of the list. Anything
// else (allOf responses, several arrays, no count) is left unpaged: offering --all
// for a shape fft has to guess at is how a wrong answer looks like a right one.
func pagedList(method string, params []param, responses *openapi3.Responses) (bool, string) {
	if method != "GET" || responses == nil {
		return false, ""
	}

	var after, size bool
	for _, p := range params {
		if p.In != openapi3.ParameterInQuery {
			continue
		}
		switch {
		case p.Name == "startAfterId" && p.Type == "string":
			after = true
		case p.Name == "size" && (p.Type == "integer" || p.Type == "number"):
			size = true
		}
	}
	if !after || !size {
		return false, ""
	}

	ok := responses.Value("200")
	if ok == nil || ok.Value == nil {
		return false, ""
	}
	media := ok.Value.Content.Get(contentTypeJSON)
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		return false, ""
	}
	schema := media.Schema.Value

	if schema.Type != nil && schema.Type.Is("array") {
		return true, ""
	}

	var key string
	for name, prop := range schema.Properties {
		if prop.Value == nil || prop.Value.Type == nil || !prop.Value.Type.Is("array") {
			continue
		}
		if key != "" {
			return false, ""
		}
		key = name
	}
	if key == "" || schema.Properties["total"] == nil {
		return false, ""
	}
	return true, key
}
