package client

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// QueryTooComplexError is the API's 400 "Query too complex", with the way out the
// API's own message gets wrong.
//
// The message advises reducing the result set size, and that is the one thing that
// does not work: the cost is in the filter or sort over the whole collection, not
// in the page, so a smaller --size fails the same way. Every POST /{entity}/search
// is the same cursor search over the same kind of backend, so this is recognised
// once, here, rather than by each command that sends a search.
//
// It wraps rather than replaces the [APIError], so the message stays the API's own
// words and the exit code stays the one the status maps to: there is no exit code
// of its own, and none is wanted.
type QueryTooComplexError struct {
	// Op names the operation, as everywhere else in this package.
	Op string
	// Err is the API's answer, kept whole.
	Err *APIError

	// Walk is set when the refusal cut an --all walk short; Pages and Items are how
	// far it got. Without them a page-two refusal reads as if the very first request
	// failed, and the user cannot tell a bad query from a collection that got too
	// big part-way through.
	Walk         bool
	Pages, Items int
}

func (e *QueryTooComplexError) Error() string {
	if !e.Walk {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: after %s (%s) of an --all walk: %s",
		e.Op, count(e.Pages, "page"), count(e.Items, "item"), e.Err)
}

func (e *QueryTooComplexError) Unwrap() error { return e.Err }

// Hint is true for every entity. A command with a better narrow filter than "an id"
// replaces it with its own.
func (e *QueryTooComplexError) Hint() string {
	return "A smaller page size (--size, or the body's \"size\") will not help: the API's cost is in the filter or sort over the whole collection, not in the page.\n" +
		"Narrow the query instead: filter on an exact match (an id or a tenant id), and drop any date range and the sort."
}

// ExplainQueryTooComplex turns the API's "Query too complex" 400 into a
// [*QueryTooComplexError] and passes every other error through untouched.
//
// It matches the message, not the status alone: an unknown enum value gets the same
// 400, and hinting at a narrower filter for it would send the user the wrong way.
func ExplainQueryTooComplex(op string, err error) error {
	var already *QueryTooComplexError
	if errors.As(err, &already) {
		return err
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		return err
	}
	if !strings.Contains(strings.ToLower(apiErr.Error()), "query too complex") {
		return err
	}
	return &QueryTooComplexError{Op: op, Err: apiErr}
}
