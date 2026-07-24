package emulator

import (
	"regexp"
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/api"
)

// routeKind is how the emulator answers an operation: from live in-memory state (the
// CRUD and search kinds) or from a canned synthesized response (kindStateless).
type routeKind int

const (
	// kindStateless is answered from the operation's SampleResponse — reachable, but
	// not remembered.
	kindStateless routeKind = iota

	kindList   // GET       /api/{coll}
	kindCreate // POST      /api/{coll}
	kindGet    // GET       /api/{coll}/{id}
	kindUpdate // PUT|PATCH /api/{coll}/{id}
	kindDelete // DELETE    /api/{coll}/{id}
	kindSearch // POST      /api/{coll}/search
)

// singletons are the top-level /api/{name} paths that look like a collection and
// are not one: the spec declares a single object, and gives them no create, no item
// route and no search. Served from the store they answer a list envelope keyed by
// the path segment — GET /api/status answered {"status": [], "total": 0}, which
// `fft ping` could not decode at all, the key having collided with the field it
// reads. So they are answered from the spec's own sample instead.
//
// Every other single-segment path is a page of entities and stays stateful, even
// the ones the spec gives no create: --seed fills a collection by file name, not by
// what the spec allows, so inferring "no POST means not a collection" would break
// seeding /api/brands and the rest of the read-only reference data.
//
// route_test.go's census fails the build when the spec grows a single-segment path
// that is in neither this map nor knownCollections, because a new singleton is
// otherwise served as a fake empty collection and nobody finds out.
var singletons = map[string]bool{
	"status":            true, // {"status":"UP"} — the health endpoint `fft ping` calls
	"health":            true, // {"status":"UP","dependencies":[…]} — the deeper check
	"routingplansgraph": true, // one graph: {"nodes":[…],"edges":[…]}
	"supportedevents":   true, // one object; the spec's example for it is bare {}
	"process":           true, // one process, found by query parameter (orderRef, pickJobRef, …)
}

// classify decides how the emulator serves an operation, and for the stateful kinds
// the collection it reads or writes and the name of the id path parameter.
//
// Only the top-level REST collections are stateful: /api/{coll}, /api/{coll}/{id},
// and the cursor search at /api/{coll}/search. Anything nested or otherwise shaped
// (/api/{coll}/{id}/sub, calculators, action endpoints) is answered from the canned
// response — the whole surface stays addressable, only the plain collections are
// remembered.
func classify(op api.Operation) (collection, idParam string, kind routeKind) {
	const apiPrefix = "/api/"
	if !strings.HasPrefix(op.Path, apiPrefix) {
		return "", "", kindStateless
	}

	// The segments after "/api/": ["facilities"], ["facilities", "{id}"], or
	// ["facilities", "search"]. Anything deeper is nested and not stateful.
	parts := pathSegments(strings.TrimPrefix(op.Path, apiPrefix))
	if len(parts) == 0 {
		return "", "", kindStateless
	}

	coll := parts[0]
	// A collection segment that is itself a parameter (/api/{something}) is not one
	// this emulator can key state by.
	if isParam(coll) {
		return "", "", kindStateless
	}
	if singletons[coll] {
		return "", "", kindStateless
	}

	switch len(parts) {
	case 1: // /api/{coll}
		switch op.Method {
		case "GET":
			return coll, "", kindList
		case "POST":
			return coll, "", kindCreate
		}
	case 2: // /api/{coll}/{x}
		last := parts[1]
		switch {
		case last == "search" && op.Method == "POST":
			return coll, "", kindSearch
		case isParam(last):
			id := paramName(last)
			switch op.Method {
			case "GET":
				return coll, id, kindGet
			case "PUT", "PATCH":
				return coll, id, kindUpdate
			case "DELETE":
				return coll, id, kindDelete
			}
		}
	}
	return "", "", kindStateless
}

// pathSegments splits a path into its non-empty segments: "/api/facilities/{id}" →
// ["api", "facilities", "{id}"].
func pathSegments(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
}

func isParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

func paramName(seg string) string {
	return strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
}

// placeholder matches a spec path template's {name} placeholders.
var placeholder = regexp.MustCompile(`\{([^}]+)\}`)

// fiberPath converts a spec path template to the syntax Fiber's router speaks:
// {facilityId} → :facilityId. The spec's parameters are all camelCase identifiers,
// which Fiber accepts verbatim.
func fiberPath(tmpl string) string {
	return placeholder.ReplaceAllString(tmpl, ":$1")
}
