package api

import (
	"slices"
	"sort"
	"strings"
	"sync"
)

// This file is hand-written. The data it describes lives in opmeta.gen.go, which
// tools/specgen produces from the OpenAPI spec — see generate.go.
//
// # Why this exists at all
//
// The generated client covers five tags: facilities, listings, stocks, health and
// user management. That is 106 methods out of the API's 557 operations. Tier-2 and
// Tier-3 commands have to reach the other 451, and they cannot do it through a
// typed client that does not have them.
//
// So they build requests from metadata instead: method, path template, parameters,
// and — the part that is easy to get wrong and impossible to notice — the
// per-parameter `explode` flag. An array query parameter is either repeated
// (a=1&a=2) or comma-joined (a=1,2) depending on what the spec says about *that
// parameter*, and applying one policy to all of them produces a request the API
// accepts and answers with the wrong rows. The whole reason [Param.Explode] is
// carried here, per parameter, is that there is no global answer to it.

// ParamIn is where a parameter travels.
type ParamIn string

// The parameter locations fft can send. The spec uses no others.
const (
	InPath   ParamIn = "path"
	InQuery  ParamIn = "query"
	InHeader ParamIn = "header"
)

// ParamType is a parameter's JSON type.
type ParamType string

// The parameter types the spec uses.
const (
	TypeString  ParamType = "string"
	TypeInteger ParamType = "integer"
	TypeNumber  ParamType = "number"
	TypeBoolean ParamType = "boolean"
	TypeArray   ParamType = "array"
)

// Param is one parameter of an operation.
type Param struct {
	// Name is the parameter's name as the API spells it: pickJobId, facilityRef.
	Name string `json:"name" yaml:"name"`

	// In is where it travels: path, query or header.
	In ParamIn `json:"in" yaml:"in"`

	// Required says the API will not accept the request without it.
	Required bool `json:"required,omitempty" yaml:"required,omitempty"`

	// Type is the parameter's JSON type.
	Type ParamType `json:"type" yaml:"type"`

	// Item is the element type when Type is [TypeArray], and "" otherwise.
	Item ParamType `json:"item,omitempty" yaml:"item,omitempty"`

	// Explode is how a multi-valued parameter is encoded: true repeats the name
	// (status=A&status=B), false joins the values with commas (status=A,B).
	//
	// It is meaningful only for an array. It is taken from the parameter's own
	// `explode` (defaulting to the `form` style's true), because the spec answers
	// it differently for different parameters — pickjob `status` is comma-joined,
	// facility `status` is repeated — and getting it wrong is silent: the API
	// answers 200 and filters on something else.
	Explode bool `json:"explode,omitempty" yaml:"explode,omitempty"`

	// Enum lists the values the API accepts, or is empty when it accepts any.
	Enum []string `json:"enum,omitempty" yaml:"enum,omitempty"`

	// Description is the parameter's prose, HTML stripped.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Operation is one operation of the fulfillmenttools API, keyed by its operationId.
type Operation struct {
	// ID is the operationId: the name `fft api <id>` takes.
	ID string

	// Method is the HTTP method, upper-case.
	Method string

	// Path is the path template, with {placeholders}: /api/pickjobs/{pickJobId}.
	Path string

	// Tags are the spec's tags. The first one decides which command group the
	// generated command lands in.
	Tags []string

	// Summary is the operation's one-line description.
	Summary string

	// Description is its prose, HTML stripped and whitespace collapsed.
	Description string

	// Permissions are the operation's x-fft-permissions, empty when it declares
	// none. 303 of the 557 operations declare them.
	Permissions []string

	// Params are the operation's parameters, sorted: path first, then query, then
	// header, alphabetically within each.
	Params []Param

	// HasBody says the operation takes a request body.
	HasBody bool

	// BodyRequired says the API will not accept the request without one.
	BodyRequired bool

	// SampleBody is a request body synthesized from the schema — the spec has 1,556
	// field-level examples and not one request-body example, so there was nothing to
	// copy and this was built. It is "" when the operation takes no body.
	SampleBody string

	// SampleResponse is a success (2xx) response body synthesized from the schema the
	// same way SampleBody is, and "" when the operation declares no JSON response body
	// (a 204, or a non-JSON one). The spec carries no response examples either, so this
	// is built. It backs `fft emulator`, which serves it for any operation it does not
	// answer from live in-memory state.
	SampleResponse string

	// Deprecated says the spec marks the operation deprecated.
	Deprecated bool
}

// Param returns the operation's parameter of that name.
func (o Operation) Param(name string) (Param, bool) {
	for _, p := range o.Params {
		if p.Name == name {
			return p, true
		}
	}
	return Param{}, false
}

// ParamsIn returns the operation's parameters that travel in one place.
func (o Operation) ParamsIn(in ParamIn) []Param {
	var out []Param
	for _, p := range o.Params {
		if p.In == in {
			out = append(out, p)
		}
	}
	return out
}

// Tag is the tag the operation's command group is derived from, or "" when the
// spec gives it none.
func (o Operation) Tag() string {
	if len(o.Tags) == 0 {
		return ""
	}
	return o.Tags[0]
}

// Operations returns every operation in the spec, sorted by ID.
//
// The returned slice is a copy, but the slices inside each [Operation] are not:
// treat an Operation as read-only.
func Operations() []Operation { return slices.Clone(operations) }

// index is the operationId lookup, built once on first use rather than in an
// init() — a `fft version` should not pay for a map of 557 entries it will not
// read.
var index = sync.OnceValue(func() map[string]Operation {
	m := make(map[string]Operation, len(operations))
	for _, op := range operations {
		m[op.ID] = op
	}
	return m
})

// LookupOperation returns the operation with that operationId. The match is exact:
// a near miss is a job for [SuggestOperations], which can say what was meant.
func LookupOperation(id string) (Operation, bool) {
	op, ok := index()[id]
	return op, ok
}

// Tags returns every tag in the spec, sorted.
var Tags = sync.OnceValue(func() []string {
	seen := make(map[string]bool)
	var out []string
	for _, op := range operations {
		for _, tag := range op.Tags {
			if !seen[tag] {
				seen[tag] = true
				out = append(out, tag)
			}
		}
	}
	sort.Strings(out)
	return out
})

// OperationsByTag returns every operation carrying tag, sorted by ID. The match is
// case-insensitive and by substring, so --tag picking finds "Picking (Operations)"
// without the user having to reproduce the parentheses.
func OperationsByTag(tag string) []Operation {
	needle := strings.ToLower(strings.TrimSpace(tag))
	if needle == "" {
		// strings.Contains(anything, "") is true, so an empty tag would quietly match
		// every operation. "No tag" is not "every tag".
		return nil
	}

	var out []Operation
	for _, op := range operations {
		for _, t := range op.Tags {
			if strings.Contains(strings.ToLower(t), needle) {
				out = append(out, op)
				break
			}
		}
	}
	return out
}

// maxSuggestions is how many "did you mean" candidates are worth showing. More
// than a handful is not a suggestion, it is a list.
const maxSuggestions = 5

// SuggestOperations returns the operationIds closest to id, best first — what
// `fft api getPickjob` should be told when it meant getPickJob.
//
// Ranking is: a case-insensitive exact match, then a substring match, then edit
// distance. Candidates further than a third of the name away are dropped: a
// suggestion nobody meant is worse than none.
func SuggestOperations(id string) []string {
	needle := strings.ToLower(strings.TrimSpace(id))
	if needle == "" {
		return nil
	}

	// The threshold scales with the name: two typos in "getPickJob" is a typo, two
	// typos in "id" is a different word.
	limit := max(len(needle)/3, 2)

	type candidate struct {
		id    string
		score int
	}
	var found []candidate

	for _, op := range operations {
		lower := strings.ToLower(op.ID)

		switch {
		case lower == needle:
			found = append(found, candidate{op.ID, 0})
		case strings.Contains(lower, needle), strings.Contains(needle, lower):
			found = append(found, candidate{op.ID, 1})
		default:
			if d := distance(needle, lower); d <= limit {
				// +1 so that no edit distance can outrank a substring match.
				found = append(found, candidate{op.ID, d + 1})
			}
		}
	}

	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score < found[j].score
		}
		return found[i].id < found[j].id
	})

	out := make([]string, 0, min(len(found), maxSuggestions))
	for _, c := range found[:min(len(found), maxSuggestions)] {
		out = append(out, c.id)
	}
	return out
}

// distance is the Levenshtein edit distance between a and b, computed with two
// rows rather than a full matrix: this runs 557 times per suggestion.
func distance(a, b string) int {
	ar, br := []rune(a), []rune(b)

	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}
