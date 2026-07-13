package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// The raw request path: how fft reaches the 451 operations the typed client does
// not have.
//
// oapi-codegen is filtered to five tags, so it has a Go method for 106 of the
// API's 557 operations. Tier-2 and Tier-3 commands reach the rest by building the
// request from the spec metadata in internal/api — method, path template,
// parameters, body.
//
// It goes through [Client.Do] like everything else. That is not a detail: Do is
// where the 401 refresh-and-retry-once lives, where a 429 is honoured, where a 500
// is retried for a GET and *not* for a POST, and where the error envelope is
// decoded as the JSON array it is. A second HTTP stack would have none of that, and
// would be a second place for a create to be silently re-sent.

// placeholder matches a {name} in a path template.
var placeholder = regexp.MustCompile(`\{([^{}]+)\}`)

// QueryParam is one query parameter, encoded the way its own spec entry says.
type QueryParam struct {
	// Name is the parameter's name as the API spells it.
	Name string

	// Values are the values to send. One for a scalar; any number for an array.
	Values []string

	// Explode chooses the encoding, and it must come from the *parameter's* own
	// `explode` in the spec — never from a policy applied to all of them.
	//
	//	true  repeats the name:     status=OPEN&status=CLOSED
	//	false joins with a comma:   status=OPEN%2CCLOSED
	//
	// The comma really does go over the wire percent-encoded: the values become one
	// string, and url.Values.Encode escapes the separator along with everything else.
	// It is what oapi-codegen's runtime already sends for the five tags it covers, and
	// it was confirmed against the live tenant on 2026-07-13 — getStockSummaries
	// filtered by facilityStatus=ONLINE%2COFFLINE answers 384,279 of 383,484 rows,
	// which is the union of the two statuses and emphatically not the zero an
	// unrecognised filter would have returned.
	//
	// The API does not reject the wrong *choice*, though, and that is the danger. It
	// answers 200 and filters on something else — a comma-joined value sent to an
	// exploded parameter is matched as the literal string "ONLINE,OFFLINE", which no
	// row has, and the user is told there are none. 17 of the spec's 77 array query
	// parameters want the joined form and 60 want the repeated one, which is why this
	// is a field and not a constant.
	Explode bool
}

// appendTo encodes the parameter into v.
func (p QueryParam) appendTo(v url.Values) {
	switch {
	case len(p.Values) == 0:
		// A parameter with no values is one the user did not give. Sending `?status=`
		// would filter on the empty string — which is why cmd/fft refuses an empty
		// value rather than letting it arrive here as no value at all.
		return

	case p.Explode:
		for _, value := range p.Values {
			v.Add(p.Name, value)
		}

	default:
		v.Set(p.Name, strings.Join(p.Values, ","))
	}
}

// RawRequest is a request described by spec metadata rather than by a typed method.
type RawRequest struct {
	// Method is the HTTP method, upper-case.
	Method string

	// Path is the path template, placeholders and all: /api/pickjobs/{pickJobId}.
	// [RawRequest.URL] fills them from PathParams, so that the escaping happens in
	// one place instead of at every call site.
	Path string

	// PathParams fills the template's placeholders.
	PathParams map[string]string

	// Query is the query string, each parameter carrying its own encoding.
	Query []QueryParam

	// Header adds request headers. Content-Type, Accept and Authorization are set by
	// the client and should not be given here.
	Header map[string]string

	// Body is the request body, or nil for none.
	Body []byte
}

// URL resolves the request against a base URL.
//
// Every placeholder must have a value and no value may be empty: an unfilled
// {pickJobId} would be sent literally and a blank one would collapse the path to
// /api/pickjobs/, which is a different endpoint that answers 200. Both are refused
// here rather than sent.
func (r RawRequest) URL(base string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("the project has no base URL")
	}

	var missing []string
	used := make(map[string]bool, len(r.PathParams))

	path := placeholder.ReplaceAllStringFunc(r.Path, func(match string) string {
		name := match[1 : len(match)-1]

		value, ok := r.PathParams[name]
		if !ok || value == "" {
			missing = append(missing, name)
			return match
		}
		used[name] = true

		// PathEscape leaves a colon alone, which matters: every facility path parameter
		// also accepts urn:fft:facility:tenantFacilityId:<id>, and escaping the colons
		// would turn that into an id nothing matches.
		return url.PathEscape(value)
	})

	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("the path %s has no value for %s", r.Path, strings.Join(missing, ", "))
	}

	// A path parameter that is not in the template cannot be sent anywhere. Silently
	// dropping it is how a user's filter goes missing.
	var unknown []string
	for name := range r.PathParams {
		if !used[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return "", fmt.Errorf("the path %s has no placeholder for %s", r.Path, strings.Join(unknown, ", "))
	}

	u, err := url.Parse(strings.TrimSuffix(base, "/") + path)
	if err != nil {
		return "", fmt.Errorf("build the URL for %s: %w", r.Path, err)
	}

	values := url.Values{}
	for _, p := range r.Query {
		p.appendTo(values)
	}
	u.RawQuery = values.Encode()

	return u.String(), nil
}

// DoRaw issues a request built from spec metadata, through the same [Client.Do] as
// everything else: the same retry rules, the same 401 refresh, the same error
// envelope.
//
// op names the operation in errors, as it does everywhere else in this package.
func (c *Client) DoRaw(ctx context.Context, op string, req RawRequest) (*Result, error) {
	if c == nil {
		return nil, fmt.Errorf("%s: there is no API client", op)
	}
	if req.Method == "" {
		return nil, fmt.Errorf("%s: no HTTP method", op)
	}

	target, err := req.URL(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return c.Do(ctx, op, func(ctx context.Context) (*http.Response, error) {
		// A fresh body reader on every attempt. Do may call this twice, and a Reader
		// the first attempt consumed would send the second one an empty body — which
		// for a PUT is a request that erases the entity it meant to update.
		var body io.Reader
		if req.Body != nil {
			body = bytes.NewReader(req.Body)
		}

		r, err := http.NewRequestWithContext(ctx, req.Method, target, body)
		if err != nil {
			return nil, err
		}

		r.Header.Set("Accept", contentTypeJSON)
		if req.Body != nil {
			r.Header.Set("Content-Type", contentTypeJSON)
		}
		for name, value := range req.Header {
			r.Header.Set(name, value)
		}

		return c.hc.Do(r)
	})
}
