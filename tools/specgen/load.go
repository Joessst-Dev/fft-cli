package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// The extension the API declares its required permissions in. 303 of the 557
// operations carry one; it is a plain array of strings at operation level.
const permissionsExtension = "x-fft-permissions"

// operation mirrors api.Operation. It is declared here rather than imported from
// internal/api on purpose: if specgen imported the package it generates into, a
// generated file that does not compile could not be regenerated, and the only way
// out of that would be to hand-edit it.
type operation struct {
	ID           string
	Method       string
	Path         string
	Tags         []string
	Summary      string
	Description  string
	Permissions  []string
	Params       []param
	HasBody      bool
	BodyRequired bool
	SampleBody   string
	Deprecated   bool
}

type param struct {
	Name        string
	In          string
	Required    bool
	Type        string
	Item        string
	Explode     bool
	Enum        []string
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

			ops = append(ops, operation{
				ID:           src.OperationID,
				Method:       method,
				Path:         tmpl,
				Tags:         append([]string(nil), src.Tags...),
				Summary:      prose(src.Summary),
				Description:  prose(src.Description),
				Permissions:  permissions(src.Extensions),
				Params:       params,
				HasBody:      src.RequestBody != nil,
				BodyRequired: src.RequestBody != nil && src.RequestBody.Value != nil && src.RequestBody.Value.Required,
				SampleBody:   sample,
				Deprecated:   src.Deprecated,
			})
		}
	}

	sort.Slice(ops, func(i, j int) bool { return ops[i].ID < ops[j].ID })
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
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
