package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Path addresses one place inside a request body: a list of segments, outermost
// first.
//
// The grammar is dotted — order.consumer.email — with a numeric segment
// addressing an array element. A backslash escapes the next character, so a key
// that genuinely contains a dot is reachable as customAttributes.order\.source.
// The API's customAttributes maps carry caller-chosen keys, and "that field
// cannot be addressed" would be a worse answer than one backslash.
//
// The grammar is deliberately not JSON Pointer. A pointer's leading slash and
// ~0/~1 escapes are a second thing to learn for a CLI whose users are already
// typing dotted paths at jq.
type Path []string

// maxPathLen and maxPathSegments bound a path. A request body a person
// actually writes by hand never needs either anywhere close to this: they exist
// so that a committed template — arriving via git clone, i.e. as untrusted
// input — cannot declare a pathologically deep or long parameter path and make
// every future render of it burn CPU walking it.
const (
	maxPathLen      = 4096
	maxPathSegments = 64
)

// ParsePath reads the --set path grammar.
//
// Every failure here is a typo the user can see, so each one names the position
// rather than reporting that the path was bad.
func ParsePath(raw string) (Path, error) {
	if raw == "" {
		return nil, fmt.Errorf("a path cannot be empty")
	}
	if len(raw) > maxPathLen {
		return nil, fmt.Errorf("a path can be at most %d characters, and this one is %d", maxPathLen, len(raw))
	}

	var (
		out     Path
		seg     strings.Builder
		escaped bool
	)
	for i, r := range raw {
		switch {
		case escaped:
			// Only the two characters the grammar gives meaning to can be escaped.
			// Swallowing an unknown escape would turn `a\b` into the key `ab` and
			// leave the user hunting for a field they never named.
			if r != '.' && r != '\\' {
				return nil, fmt.Errorf(`%q is not an escape in %q: only \. and \\ are`, `\`+string(r), raw)
			}
			seg.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '.':
			if seg.Len() == 0 {
				return nil, fmt.Errorf("%q has an empty segment at position %d", raw, i+1)
			}
			if len(out) == maxPathSegments {
				return nil, fmt.Errorf("%q has more than %d segments", raw, maxPathSegments)
			}
			out = append(out, seg.String())
			seg.Reset()
		default:
			seg.WriteRune(r)
		}
	}

	if escaped {
		return nil, fmt.Errorf(`%q ends in a lone backslash`, raw)
	}
	if seg.Len() == 0 {
		return nil, fmt.Errorf("%q ends in an empty segment", raw)
	}
	if len(out) == maxPathSegments {
		return nil, fmt.Errorf("%q has more than %d segments", raw, maxPathSegments)
	}
	return append(out, seg.String()), nil
}

// String renders the path back into the grammar, escapes and all, so that an
// error message quotes something the user could paste.
func (p Path) String() string {
	parts := make([]string, len(p))
	for i, seg := range p {
		r := strings.NewReplacer(`\`, `\\`, `.`, `\.`)
		parts[i] = r.Replace(seg)
	}
	return strings.Join(parts, ".")
}

// index reads a segment as an array index. The second result is false for a
// segment that is not a plain non-negative integer — "01" and "+1" included,
// because a body whose author wrote either meant a key.
func index(seg string) (int, bool) {
	if seg == "" || (len(seg) > 1 && seg[0] == '0') {
		return 0, false
	}
	for _, r := range seg {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(seg)
	if err != nil {
		return 0, false
	}
	return n, true
}

// Apply sets value at path inside doc and returns the document.
//
// It returns the root rather than mutating it because appending to a top-level
// array produces a new slice header, and a function that silently kept the old
// one would drop the write it just reported making.
func Apply(doc any, path Path, value any) (any, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("a path cannot be empty")
	}
	return set(doc, path, value, nil)
}

// donePath is the path already traversed, threaded through set as a cons list
// rather than a slice rebuilt at every level.
//
// The old `append(append(Path{}, done...), seg)` copied the whole prefix walked
// so far at every single level, which made a render O(depth²) even though the
// copy is only ever read back on the rare path that produces an error — with
// maxPathSegments this is no longer enough to matter on its own, but there is
// no reason to keep paying for it. Pushing is O(1); [donePath.path] walks the
// list and materializes a real Path, only when an error actually needs one.
type donePath struct {
	seg    string
	parent *donePath
}

func (d *donePath) push(seg string) *donePath {
	return &donePath{seg: seg, parent: d}
}

// path materializes the segments walked so far, outermost first. A nil
// receiver — the root, nothing walked yet — is the empty path.
func (d *donePath) path() Path {
	var segs []string
	for p := d; p != nil; p = p.parent {
		segs = append(segs, p.seg)
	}
	slices.Reverse(segs)
	return Path(segs)
}

func (d *donePath) String() string { return d.path().String() }

// subject names the part of the document an error is about.
//
// At the root nothing has been walked yet, so [donePath.String] is the empty
// string and the message it builds loses its subject: " has 2 elements, so
// index 9 is past its end" reads as a rendering bug rather than as the report
// that the body itself is the array that is too short.
func (d *donePath) subject() string {
	if d == nil {
		return "the body"
	}
	return d.String()
}

// set walks one segment and recurses. done is the prefix already traversed, and
// exists only so that an error can name the part of the path that went wrong
// rather than the whole of it.
func set(node any, path Path, value any, done *donePath) (any, error) {
	if len(path) == 0 {
		return value, nil
	}
	seg, rest := path[0], path[1:]

	switch n := node.(type) {
	case map[string]any:
		child, err := set(n[seg], rest, value, done.push(seg))
		if err != nil {
			return nil, err
		}
		n[seg] = child
		return n, nil

	case []any:
		i, ok := index(seg)
		if !ok {
			return nil, fmt.Errorf("%s is an array, and %q is not an index into one",
				done.subject(), seg)
		}
		switch {
		case i > len(n):
			// Padding with nulls would build a body the API rejects with an opaque
			// 400, which is a worse place to learn about the typo than here.
			return nil, fmt.Errorf("%s has %d %s, so index %d is past its end",
				done.subject(), len(n), plural(len(n), "element"), i)
		case i == len(n):
			// Appending at exactly the end is how a caller builds a list up one
			// --set at a time, which is the only ergonomic way to do it.
			child, err := set(nil, rest, value, done.push(seg))
			if err != nil {
				return nil, err
			}
			return append(n, child), nil
		default:
			child, err := set(n[i], rest, value, done.push(seg))
			if err != nil {
				return nil, err
			}
			n[i] = child
			return n, nil
		}

	case nil:
		// Absent, or an explicit null the API sent for a field that has no value
		// yet. Either way the user asking for something inside it means "make
		// one" — this is subDoc's rule, generalised to arrays.
		var fresh any
		if _, isIndex := index(seg); isIndex {
			fresh = []any{}
		} else {
			fresh = map[string]any{}
		}
		return set(fresh, path, value, done)

	default:
		// A scalar. Replacing it with a container would delete a field the user
		// still has, silently, on what is nearly always a typo.
		return nil, fmt.Errorf("%s is %s, so %s has nowhere to go",
			done.subject(), describe(node), done.push(seg).String())
	}
}

// describe names a value's JSON type for an error message.
func describe(v any) string {
	switch v.(type) {
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case json.Number, float64:
		return "a number"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// Set is one --set as the user gave it: the key they named (a declared parameter
// or a bare path) and the value it takes.
type Set struct {
	Key   string
	Value any
}

// ParseValue reads a --set value.
//
// A value that parses as JSON is that JSON — so 3 is a number, true is a
// boolean, and {"a":1} is an object — and anything else is the string it looks
// like. Numbers arrive as json.Number, which marshals back as the digits that
// were typed, so a 19-digit id set here reaches the API unrounded.
//
// The ambiguous case is a string that looks like JSON. `--set qty='"3"'` is the
// escape hatch, and --set-string is the one that does not need shell quoting.
func ParseValue(raw string) any {
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	// Trailing content means this was never one JSON value: "3 4" is a string.
	if dec.More() {
		return raw
	}
	return v
}
