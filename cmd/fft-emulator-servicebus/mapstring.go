package main

// mapString reads a string field out of a decoded target document.
//
// A target is stored exactly as it was registered, so every field is whatever JSON
// made of it. A field that is absent, null, or a number where a name was expected is
// the empty string — the callers all treat "missing" and "not a usable name" the
// same way, and there is nothing this transport could do differently about the two.
func mapString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
