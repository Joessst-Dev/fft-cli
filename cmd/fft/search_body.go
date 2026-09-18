package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// rawSearch is a search whose query and sort are the user's own bytes. The paging
// fields around them are fft's to set.
type rawSearch = client.SearchPayload[json.RawMessage, json.RawMessage]

// searchPayload reads a `search --file` body, refuses one the API would not
// understand, and returns it with the query and sort exactly as they were written.
//
// The body is checked against the generated schema Q and S, but never sent through
// it. Those models are lossy: a NumberFilter is a float32, so `"notEq": 16777217`
// would go out as 16777216 and match what the user excluded. The check is
// DisallowUnknownFields — the API answers `{"statuz": …}` with a 200 listing
// everything, a filter that silently does not filter — and [checkSearchKeys], which
// closes the gaps encoding/json leaves open.
func searchPayload[Q, S any](deps *Deps, path, entity string) (rawSearch, error) {
	var payload rawSearch

	raw, err := readBody(deps, path)
	if err != nil {
		return payload, err
	}
	invalid := func(err error) error {
		return exitcode.UsageError{Err: fmt.Errorf("%s is not a valid %s search: %w", path, entity, err)}
	}

	var typed client.SearchPayload[Q, S]
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&typed); err != nil {
		return payload, invalid(err)
	}
	if err := checkSearchKeys(raw, reflect.TypeFor[client.SearchPayload[Q, S]]()); err != nil {
		return payload, invalid(err)
	}

	// The top-level keys are now known to be spelled exactly as the payload's tags,
	// and its own fields are a string, an int and a bool, so decoding into it loses
	// nothing; the query and the sort stay bytes.
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, invalid(err)
	}
	if len(payload.Query) == 0 {
		// An absent query is how a file asks for everything, and the API wants that
		// said as {}.
		payload.Query = json.RawMessage(`{}`)
	}
	return payload, nil
}

var jsonUnmarshaler = reflect.TypeFor[json.Unmarshaler]()

// checkSearchKeys refuses what a strict decode into schema accepts but the API
// would read differently, now that the bytes are sent as written:
//
//   - a key in another case. encoding/json matches `"VALUE"` to the field value;
//     the API would not, and would not filter.
//   - a key given twice. encoding/json keeps the last; what the API keeps is its
//     own business, and a query that means one of two things is refused.
//   - null where the schema has a type. The model cannot tell null from absent, so
//     it validated nothing, and a filter the API may read as "no filter" is the
//     silent wrong answer DisallowUnknownFields exists to prevent.
//
// Where the schema is opaque to reflection — a union with its own UnmarshalJSON, a
// map of anything — only duplicates can be checked.
func checkSearchKeys(raw []byte, schema reflect.Type) error {
	w := keyWalker{dec: json.NewDecoder(bytes.NewReader(raw))}
	w.dec.UseNumber()
	return w.value(knownSchema(schema), "")
}

type keyWalker struct {
	dec *json.Decoder
}

// value walks the next JSON value, which the schema types as t (nil if unknown).
func (w keyWalker) value(t reflect.Type, at string) error {
	tok, err := w.dec.Token()
	if err != nil {
		return err
	}

	switch tok {
	case nil:
		if t != nil {
			return fmt.Errorf("%s is null: leave it out instead", describePath(at))
		}
	case json.Delim('{'):
		return w.object(t, at)
	case json.Delim('['):
		var elem reflect.Type
		if t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
			elem = knownSchema(t.Elem())
		}
		return w.array(elem, at)
	}
	return nil
}

func (w keyWalker) object(t reflect.Type, at string) error {
	var fields map[string]reflect.Type
	if t != nil && t.Kind() == reflect.Struct {
		fields = jsonFields(t)
	}

	seen := make(map[string]bool)
	for w.dec.More() {
		tok, err := w.dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("%s has a key that is not a string", describePath(at))
		}

		path := key
		if at != "" {
			path = at + "." + key
		}
		if seen[key] {
			return fmt.Errorf("%s is given twice", path)
		}
		seen[key] = true

		var child reflect.Type
		switch {
		case fields != nil:
			ft, ok := fields[key]
			if !ok {
				return misspelled(path, key, fields)
			}
			child = knownSchema(ft)
		case t != nil && t.Kind() == reflect.Map:
			child = knownSchema(t.Elem())
		}

		if err := w.value(child, path); err != nil {
			return err
		}
	}
	_, err := w.dec.Token()
	return err
}

func (w keyWalker) array(elem reflect.Type, at string) error {
	for i := 0; w.dec.More(); i++ {
		if err := w.value(elem, at+"["+strconv.Itoa(i)+"]"); err != nil {
			return err
		}
	}
	_, err := w.dec.Token()
	return err
}

// misspelled names the field key was taken for. The strict decode already refused
// a key that matches no field in any case, so the second message is a guard.
func misspelled(path, key string, fields map[string]reflect.Type) error {
	for name := range fields {
		if strings.EqualFold(name, key) {
			return fmt.Errorf("%s: the field is spelled %q", path, name)
		}
	}
	return fmt.Errorf("%s is not a field of the search", path)
}

func describePath(at string) string {
	if at == "" {
		return "the search"
	}
	return at
}

// knownSchema is t without its pointers, or nil when reflection cannot say what
// JSON t accepts: an interface, or a type that decodes itself.
func knownSchema(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() == reflect.Interface || reflect.PointerTo(t).Implements(jsonUnmarshaler) {
		return nil
	}
	return t
}

// jsonFields maps each JSON name of struct t to its field's type, as encoding/json
// names them — embedded structs without a tag contribute their own fields.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for f := range t.Fields() {
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")

		if f.Anonymous && name == "" {
			if inner := knownSchema(f.Type); inner != nil && inner.Kind() == reflect.Struct {
				maps.Copy(fields, jsonFields(inner))
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = f.Type
	}
	return fields
}
