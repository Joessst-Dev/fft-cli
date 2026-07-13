package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// contentTypeJSON is what every fulfillmenttools request body is.
const contentTypeJSON = "application/json"

// entityDoc is an API entity as JSON, decoded but not modelled.
//
// Every mutating command works on this rather than on the generated models, for
// two reasons. The generated models drop fields: the swagger repeatedly builds a
// schema as an allOf with sibling properties, which oapi-codegen collapses — so
// api.Facility has no `id` (though every response carries one) and
// api.ManagedFacilityForCreation has no `address` (though it is required). And a
// PUT that round-tripped through a lossy struct would silently *delete* the
// fields the struct forgot: data loss wearing a type system.
//
// So an entity is carried as the API sent it, and only the fields fft actually
// changes are touched.
type entityDoc map[string]any

// decodeDoc reads an entity (or a request body) as a document.
//
// UseNumber keeps every number exactly as written. Without it a version of
// 9007199254740993 becomes a float64 and comes back as 9007199254740992, which
// is the kind of bug that only ever bites in production.
func decodeDoc(raw []byte, what string) (entityDoc, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var doc entityDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode %s: %w", what, err)
	}
	if doc == nil {
		return nil, fmt.Errorf("decode %s: it is null, not a JSON object", what)
	}
	return doc, nil
}

// docVersion reads the optimistic-locking version off an entity. what names the
// entity in the failure, because "it has no version" is only actionable if the
// user is told what "it" was.
func docVersion(doc entityDoc, what string) (int, error) {
	n, ok := doc["version"].(json.Number)
	if !ok {
		return 0, fmt.Errorf("the %s has no version, so it cannot be updated safely", what)
	}

	v, err := n.Int64()
	if err != nil {
		return 0, fmt.Errorf("the %s's version %q is not a whole number: %w", what, n, err)
	}
	return int(v), nil
}

// docString reads a string field, or "" when it is absent or is not one.
func docString(doc entityDoc, key string) string {
	s, _ := doc[key].(string)
	return s
}

// tenantClient resolves the active project and builds an authenticated client
// for it — the first three lines of every command that talks to the tenant.
func tenantClient(deps *Deps) (*client.Client, error) {
	project, src, err := deps.tokenSource()
	if err != nil {
		return nil, err
	}
	return deps.apiClient(project, src)
}

// sendDoc issues a request whose body is doc, and returns the API's answer
// verbatim. The raw bytes are what -o json prints; re-encoding a decoded form
// would drop whatever fft has no field for.
//
// The answer is deliberately *not* decoded here. A 2xx with an empty body is a
// legitimate answer — the actions endpoint and some PATCHes give one — and
// failing to parse it would fail a command whose write had already landed, whose
// natural remedy is to run the mutation a second time. On a create, that means a
// duplicate. Nothing needs the decoded form anyway: the callers render the bytes.
func sendDoc(ctx context.Context, c *client.Client, op string, body []byte,
	send func(ctx context.Context, body io.Reader) (*http.Response, error),
) ([]byte, error) {
	res, err := c.Do(ctx, op, func(ctx context.Context) (*http.Response, error) {
		// A fresh Reader per attempt: Do may call this twice, and a Reader consumed
		// by the first attempt would send an empty body on the second.
		return send(ctx, bytes.NewReader(body))
	})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// sendEntity marshals doc and sends it. It is sendDoc for the common case where
// the body is a document fft assembled rather than bytes it read from a file.
func sendEntity(ctx context.Context, c *client.Client, op string, doc entityDoc,
	send func(ctx context.Context, body io.Reader) (*http.Response, error),
) ([]byte, error) {
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("%s: encode the request: %w", op, err)
	}
	return sendDoc(ctx, c, op, body, send)
}

// readBody reads a request body from a file, or from stdin when path is "-".
func readBody(deps *Deps, path string) ([]byte, error) {
	var (
		raw []byte
		err error
	)

	if path == "-" {
		raw, err = io.ReadAll(deps.In)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, exitcode.UsageError{Err: fmt.Errorf("read %s: %w", path, err)}
	}

	if !json.Valid(raw) {
		return nil, exitcode.UsageError{Err: fmt.Errorf("%s does not contain valid JSON", path)}
	}
	return raw, nil
}

// enumValue normalises a user's flag value to the API's spelling, or says which
// values it would have accepted. Case is forgiven: --status active is what a
// human types, and refusing it teaches nothing.
func enumValue(flag, v string, allowed []string) (string, error) {
	up := strings.ToUpper(strings.TrimSpace(v))
	for _, a := range allowed {
		if up == a {
			return a, nil
		}
	}
	return "", exitcode.UsageError{Err: fmt.Errorf("unknown --%s value %q: want one of %s",
		flag, v, strings.Join(allowed, ", "))}
}

// field renders an absent value as a dash. An empty cell reads as a rendering
// bug; a dash reads as "the API did not send one", which is what it means.
func field(style output.Style, v string) string {
	if v == "" {
		return style.Faint("-")
	}
	return v
}

// ptr is the one-liner the generated models need everywhere: their optional
// fields are pointers, and Go has no address-of for a literal.
func ptr[T any](v T) *T { return &v }
