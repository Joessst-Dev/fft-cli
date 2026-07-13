package main

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

var (
	// breaks are the tags that stand for a line break. They become a space, because
	// the sentences on either side of them are two sentences.
	breaks = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</li>|</div>`)

	// tags matches any other HTML tag. It is deleted rather than spaced out: the spec
	// writes `<a href="…">the docs</a>.` and a space would leave "the docs ." with a
	// gap before the full stop.
	tags = regexp.MustCompile(`<[^>]*>`)

	// space collapses the runs of whitespace that stripping the tags leaves behind.
	space = regexp.MustCompile(`\s+`)
)

// The bounds that make the synthesizer terminate.
//
// The spec's schemas are recursive — a Facility references a Facility, an action
// references an action — so a walk that follows every $ref does not stop. Two
// things stop it: a schema already on the current branch is not entered a second
// time, and the walk gives up at maxDepth whatever it finds.
const (
	// maxDepth is how deep a sample body nests. Six is past the point where a human
	// would edit the value by hand anyway.
	maxDepth = 6

	// maxArray is how many elements a sample array carries. One is enough to show
	// the shape; a second is the same shape again.
	maxArray = 1
)

// contentTypeJSON is the only request body fft sends.
const contentTypeJSON = "application/json"

// synthesizer builds a sample request body from a schema.
//
// The spec has 1,556 field-level `example:` values and zero request-body examples,
// so there is nothing to copy: every body fft shows under `--example` is built
// here. The rules, in the order they are tried for any one value:
//
//  1. the schema's own `example`, then its `default`, then its first `enum` value;
//  2. failing all three, a placeholder chosen from `type` and `format` — an RFC3339
//     instant for a date-time, a zero UUID for a uuid, and so on.
//
// A field is emitted when it is `required`, or when it is optional *and* carries an
// `example` — an optional field with nothing to say about itself would only be a
// null for the user to delete.
type synthesizer struct{}

// body returns the sample body for a request body, or "" when there is none to
// build: an operation with no body at all, or one whose only content type is not
// JSON.
func (s *synthesizer) body(ref *openapi3.RequestBodyRef) (string, error) {
	if ref == nil || ref.Value == nil {
		return "", nil
	}

	media := ref.Value.Content.Get(contentTypeJSON)
	if media == nil || media.Schema == nil {
		return "", nil
	}

	value := s.value(media.Schema, nil, 0)
	if value == nil {
		return "", nil
	}

	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// value builds the sample for one schema.
//
// seen holds the $refs already entered on this branch, so a schema that references
// itself terminates rather than recursing forever. It is copied on every push
// rather than appended in place: two sibling properties must not see each other's
// path.
func (s *synthesizer) value(ref *openapi3.SchemaRef, seen []string, depth int) any {
	if ref == nil || ref.Value == nil {
		return nil
	}

	if ref.Ref != "" {
		if slices.Contains(seen, ref.Ref) {
			// A recursive reference. Null says "there could be another one of these
			// here" without saying it forever.
			return nil
		}
		seen = push(seen, ref.Ref)
	}

	schema := ref.Value

	// The spec's own answer always wins, at any depth: it is a real value written by
	// someone who knows the API, and nothing synthesized can beat it — as long as it
	// is of the type the schema declares. Several are not (see coerce).
	for _, candidate := range []any{schema.Example, schema.Default} {
		if v, ok := literal(candidate); ok {
			if v, ok := coerce(v, schema); ok {
				return v
			}
		}
	}
	if len(schema.Enum) > 0 {
		if v, ok := literal(schema.Enum[0]); ok {
			if v, ok := coerce(v, schema); ok {
				return v
			}
		}
	}

	if branch, discriminant := chooseBranch(schema); branch != nil {
		// depth+1, so that termination does not rest on the $ref guard alone. Every arm
		// in this spec is a $ref today and the guard would stop it; an inline arm that
		// nested itself would not be stopped by anything else.
		out := s.value(branch, seen, depth+1)

		// A oneOf is matched by its discriminator, so a body that omits it is
		// unroutable however well-formed the rest of it is. The mapping key is what the
		// property has to hold — set it, whatever the branch's own schema said.
		if obj, ok := out.(map[string]any); ok && discriminant.property != "" {
			obj[discriminant.property] = discriminant.value
		}
		return out
	}

	switch kind(schema) {
	case "object":
		if depth >= maxDepth {
			return map[string]any{}
		}
		return s.object(schema, seen, depth)

	case "array":
		if depth >= maxDepth || schema.Items == nil {
			return []any{}
		}
		out := make([]any, 0, maxArray)
		for range maxArray {
			out = append(out, s.value(schema.Items, seen, depth+1))
		}
		return out

	default:
		return placeholder(schema)
	}
}

// object builds the sample for an object: every required property, plus the optional
// ones the spec has a concrete value for.
//
// "A concrete value" is an `example` or a `default`, and the default matters as much
// as the example: `type` on a facility is optional, has no example, and carries
// `default: MANAGED_FACILITY` — which is the discriminator the whole body is routed
// by. Emitting only the required fields would produce a create body with no type.
func (s *synthesizer) object(schema *openapi3.Schema, seen []string, depth int) map[string]any {
	props, required := flatten(schema, seen, 0)

	out := make(map[string]any, len(required))
	for _, name := range sortedKeys(props) {
		if !required[name] && !hasValue(props[name]) {
			continue
		}
		out[name] = s.value(props[name], seen, depth+1)
	}
	return out
}

// flatten merges an allOf chain into one set of properties.
//
// The spec builds most of its schemas as `allOf: [$ref: Parent]` with *sibling*
// properties, and both halves are real: dropping the siblings is exactly the bug
// that leaves the generated api.ManagedFacilityForCreation with no `address` field
// although the schema requires one. So both halves are merged here, with the outer
// schema's own properties winning a name clash — that is the specialisation the
// author meant.
func flatten(schema *openapi3.Schema, seen []string, depth int) (map[string]*openapi3.SchemaRef, map[string]bool) {
	props := make(map[string]*openapi3.SchemaRef)
	required := make(map[string]bool)

	if schema == nil || depth >= maxDepth {
		return props, required
	}

	for _, ref := range schema.AllOf {
		if ref == nil || ref.Value == nil {
			continue
		}
		// An allOf branch that leads back to a schema already on this path would
		// otherwise merge itself into itself.
		branchSeen := seen
		if ref.Ref != "" {
			if slices.Contains(seen, ref.Ref) {
				continue
			}
			branchSeen = push(seen, ref.Ref)
		}

		inherited, inheritedRequired := flatten(ref.Value, branchSeen, depth+1)
		for name, p := range inherited {
			props[name] = p
		}
		for name := range inheritedRequired {
			required[name] = true
		}
	}

	for name, p := range schema.Properties {
		props[name] = p
	}
	for _, name := range schema.Required {
		required[name] = true
	}
	return props, required
}

// discriminant is the property a oneOf is routed by, and the value that routes it
// to the branch chosen.
type discriminant struct {
	property string
	value    string
}

// chooseBranch picks one arm of a oneOf/anyOf.
//
// Where there is a discriminator mapping, the arm is the mapping's first entry by
// key — an arbitrary but stable choice, and one that comes with the value the
// discriminator property has to hold. Where there is none, it is the first arm the
// spec lists. Either way a sample body is *one* concrete thing: a union of all the
// arms would be a body the API rejects.
func chooseBranch(schema *openapi3.Schema) (*openapi3.SchemaRef, discriminant) {
	arms := schema.OneOf
	if len(arms) == 0 {
		arms = schema.AnyOf
	}
	if len(arms) == 0 {
		return nil, discriminant{}
	}

	d := schema.Discriminator
	if d == nil || len(d.Mapping) == 0 {
		return arms[0], discriminant{}
	}

	// Sorted, because Go randomises map order and CI fails the build on a diff.
	key := sortedKeys(d.Mapping)[0]
	chosen := discriminant{property: d.PropertyName, value: key}

	// A MappingRef is a SchemaRef that serialises as a bare string; the loader has
	// resolved it, so it can be matched against the arms by $ref.
	mapped := openapi3.SchemaRef(d.Mapping[key])

	for _, arm := range arms {
		if arm != nil && arm.Ref == mapped.Ref {
			return arm, chosen
		}
	}

	// The mapping names a schema the arms do not list. The mapping is the more
	// specific statement, so it wins — as long as it resolved to something.
	if mapped.Value != nil {
		return &mapped, chosen
	}

	// An unresolvable mapping. Fall back to the first arm, but still set the
	// discriminator: the property is required whichever arm the body turns out to be.
	return arms[0], chosen
}

// kind is the schema's effective type. A schema with properties but no declared
// type is an object — the spec has several.
func kind(schema *openapi3.Schema) string {
	if schema.Type != nil {
		for _, t := range []string{"object", "array", "string", "integer", "number", "boolean"} {
			if schema.Type.Is(t) {
				return t
			}
		}
	}

	switch {
	case len(schema.Properties) > 0, len(schema.AllOf) > 0:
		return "object"
	case schema.Items != nil:
		return "array"
	default:
		return ""
	}
}

// hasValue reports whether a property is worth emitting although it is optional: the
// spec gave it a concrete value, so the sample can show a real one rather than a null
// for the user to delete.
func hasValue(ref *openapi3.SchemaRef) bool {
	if ref == nil || ref.Value == nil {
		return false
	}
	return ref.Value.Example != nil || ref.Value.Default != nil
}

// coerce checks a value the spec wrote against the type the schema declares, and
// converts it where the spec contradicted itself.
//
// It does, repeatedly: fulfillmentProcessBuffer is `type: integer` with
// `example: '240'` — a *quoted* 240, so a string. Copying that example verbatim
// produces a sample body the API rejects with a 400, which is worse than having no
// sample body at all, because the user will believe it.
//
// ok is false when the value cannot be made into the declared type. The caller then
// falls through to the next candidate, and in the end to a typed placeholder.
func coerce(v any, schema *openapi3.Schema) (any, bool) {
	switch kind(schema) {
	case "integer":
		switch n := v.(type) {
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return i, true
			}
		case string:
			if i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
				return i, true
			}
		}
		return nil, false

	case "number":
		switch n := v.(type) {
		case json.Number:
			return n, true
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				return f, true
			}
		}
		return nil, false

	case "boolean":
		switch b := v.(type) {
		case bool:
			return b, true
		case string:
			if parsed, err := strconv.ParseBool(strings.TrimSpace(b)); err == nil {
				return parsed, true
			}
		}
		return nil, false

	case "string":
		switch s := v.(type) {
		case string:
			return s, true
		case json.Number:
			// A number where a string was declared: the spec meant the digits.
			return s.String(), true
		}
		return nil, false

	case "array":
		_, ok := v.([]any)
		return v, ok

	case "object":
		_, ok := v.(map[string]any)
		return v, ok

	default:
		// A schema with no declared type accepts whatever the spec wrote in it.
		return v, true
	}
}

// placeholder is the value for a scalar the spec said nothing concrete about. The
// formats matter: a date-time that is not RFC3339 and a uuid that is not a uuid are
// both rejected, and a sample body that has to be fixed before it can be sent is
// not a sample body.
func placeholder(schema *openapi3.Schema) any {
	switch kind(schema) {
	case "integer":
		return 1
	case "number":
		return 1
	case "boolean":
		return false
	case "string":
		switch schema.Format {
		case "date-time":
			return "2026-01-01T00:00:00Z"
		case "date":
			return "2026-01-01"
		case "uuid":
			return "00000000-0000-0000-0000-000000000000"
		case "email":
			return "user@example.com"
		case "uri", "url":
			return "https://example.com"
		case "byte":
			return "c3RyaW5n"
		default:
			return "string"
		}
	default:
		// A schema with no type and no shape. Null is honest: fft does not know what
		// goes here, and a made-up string would be a worse guess than none.
		return nil
	}
}

// literal reduces a value the spec wrote in YAML to one that survives a JSON round
// trip. ok is false when it does not — a value fft cannot render is one it must
// build a placeholder for instead.
func literal(v any) (any, bool) {
	if v == nil {
		return nil, false
	}

	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}

	var out any
	dec := json.NewDecoder(strings.NewReader(string(encoded)))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, false
	}
	return out, true
}

// push returns seen with ref appended, copied rather than appended in place.
//
// The copy is the point. Two sibling properties both descend from the same seen
// slice, and an append that reused its backing array would let the first one's path
// show up in the second one's — which reads as a recursion that is not there, and
// silently truncates the sample body.
func push(seen []string, ref string) []string {
	out := make([]string, len(seen)+1)
	copy(out, seen)
	out[len(seen)] = ref
	return out
}
