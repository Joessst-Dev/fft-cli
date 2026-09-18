package history

import (
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// Redacted replaces a value history does not keep.
const Redacted = "<redacted>"

// inlineBodyFlags carry a request body inline. A body is data from the tenant or
// for it, and history records none.
var inlineBodyFlags = map[string]bool{"data": true}

// pairFlags carry name=value (or, for a header, "Name: value") pairs whose name is
// worth keeping and whose value may not be: a header is where a token travels, and
// a path parameter or a template value can be anything.
var pairFlags = map[string]bool{"header": true, "set": true, "set-string": true, "param": true, "require": true}

// personalNamePatterns are name substrings of flags and query parameters that
// carry a person's data: a consumer's contact details and name, an employee's
// login, and the free-text searches such values are typed into. The list is
// narrow on purpose. Ids and paths are kept, because reopening a request needs
// them, and a name like facilityName is the tenant's, not a person's.
var personalNamePatterns = []string{
	"email", "phone", "mobile", "address", "street", "postal",
	"firstname", "lastname", "consumername", "username", "assigneduser", "searchterm",
}

// looksPersonal reports whether name is shaped like one that holds a person's
// data. Case and the separators '-' and '_' are ignored, as for credentials.
func looksPersonal(name string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(name))
	for _, pattern := range personalNamePatterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}

// Redact returns args with the values history must not keep replaced by
// [Redacted]. args are positional arguments and flags, each flag written as
// --name=value or, for a flag given without a value, a bare --name.
//
//   - --data keeps "-" and "@path", which say where the body came from; an inline
//     body is dropped.
//   - --header, --set, --param and --require keep the name of each pair.
//   - A flag whose name looks like a credential or a person's data is dropped, and
//     so is the value of any pair whose name does.
//   - Any value that starts with '{' or '[' is a JSON document, and is dropped
//     wherever it appears.
//
// args is not modified.
func Redact(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, redactArg(arg))
	}
	return out
}

func redactArg(arg string) string {
	body, isFlag := strings.CutPrefix(arg, "--")
	if !isFlag {
		if looksLikeJSON(arg) {
			return Redacted
		}
		return arg
	}

	name, value, hasValue := strings.Cut(body, "=")
	if !hasValue {
		return arg
	}
	redact := func(v string) string { return "--" + name + "=" + v }

	switch {
	case withheldName(name), looksLikeJSON(value):
		return redact(Redacted)
	case inlineBodyFlags[name]:
		if value == "-" || strings.HasPrefix(value, "@") {
			return arg
		}
		return redact(Redacted)
	case pairFlags[name]:
		if key, ok := pairName(value); ok {
			return redact(key + Redacted)
		}
		return redact(Redacted)
	}

	if key, ok := pairName(value); ok && withheldName(strings.TrimRight(key, "=: ")) {
		return redact(key + Redacted)
	}
	return arg
}

// withheldName reports whether a flag or pair of this name keeps no value.
func withheldName(name string) bool {
	return secrets.LooksLikeCredential(name) || looksPersonal(name)
}

// pairName returns the name part of a name=value or "Name: value" pair, with its
// separator, and whether value is a pair at all.
func pairName(value string) (string, bool) {
	i := strings.IndexAny(value, "=:")
	if i <= 0 {
		return "", false
	}
	end := i + 1
	if value[i] == ':' && end < len(value) && value[end] == ' ' {
		end++
	}
	return value[:end], true
}

func looksLikeJSON(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}
