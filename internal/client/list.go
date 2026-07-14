package client

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// The API's *other* pagination model.
//
// [Search] pages a POST /{entity}/search with an opaque cursor. A good deal of the
// API does not work that way: it pages a plain GET with ?startAfterId=&size=, and
// answers {"<entities>": [...], "total": n} — no pageInfo, no cursor, no
// hasNextPage. GET /api/facilities/{id}/connections is the first one fft curates,
// and it will not be the last.
//
// The two are kept apart rather than unified behind one "list" abstraction,
// because they differ in the two things a caller has to reason about. The cursor is
// the *last item's own id* rather than a token the server minted, so the paginator
// has to be able to read an id out of an entity — which is why [ListOp] carries an
// ID function and [Op] does not. And the end of the list is inferred from a short
// page rather than announced, so the last page always costs one extra request.

// DefaultListSize is what these endpoints use when `size` is absent.
//
// It is 25, and [DefaultSize] — the cursor searches' default — is 20. They are
// deliberately not folded into one number: the defaults belong to the API, not to
// fft, and a user who passed --size 25 and quietly got 20 would be right to file a
// bug. See the same argument at the top of search.go.
const DefaultListSize = 25

// ListOp binds the id-keyed pagination to one endpoint. It is [Op] for the GET
// lists, and it is used the same way.
type ListOp[T any] struct {
	// Name names the operation in errors ("list the facility's connections").
	Name string

	// Items is the response field holding the entities: "interFacilityConnections".
	Items string

	// Do issues one page. Every parameter but the paging pair — the facility whose
	// connections these are, a filter, a locale — is closed over by the constructor,
	// exactly as [Op.Do] closes over nothing but the body.
	Do func(ctx context.Context, raw api.ClientInterface, after string, size int) (*http.Response, error)

	// ID reads an entity's id, which *is* the cursor: the next page is the one that
	// starts after the last id of this one.
	//
	// So a list fft cannot page is one whose entities it cannot identify, and that is
	// a failure worth naming rather than a loop worth entering. [RawID] is the
	// implementation every caller wants.
	ID func(T) (string, error)
}

// ListPage is one page of a GET list.
type ListPage[T any] struct {
	// Items are the entities on this page.
	Items []T

	// Total is the number of matches. Unlike [Page.Total] it is not a pointer: this
	// envelope always carries a total, so there is no "the API did not count" to tell
	// apart from a count of zero.
	Total int
}

// RawID reads the id off an entity that has not been decoded into a model, which is
// what every list command actually holds — the table is built from a view struct and
// -o json prints the API's own bytes, so nothing in between needs the generated type.
func RawID(raw json.RawMessage) (string, error) {
	var entity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &entity); err != nil {
		return "", fmt.Errorf("read the entity's id: %w", err)
	}
	if entity.ID == "" {
		return "", fmt.Errorf("the API returned an entity with no id, so the next page cannot be addressed")
	}
	return entity.ID, nil
}

// List fetches one page. after is the id to start after — empty for the first page.
func List[T any](ctx context.Context, c *Client, op ListOp[T], after string, size int) (ListPage[T], error) {
	var page ListPage[T]

	if c == nil || op.Do == nil {
		return page, fmt.Errorf("%s: there is no API client", op.Name)
	}
	// fft's bounds, not the API's: unlike the cursor searches, this endpoint's schema
	// declares no minimum and no maximum on `size`. That is not a licence to pass any
	// number through — a page size nobody has bounded is a request whose cost nobody
	// can predict, and a server that quietly caps it hands back a short page that
	// [ListAll] would have to tell apart from the end of the list. Borrowing the
	// searches' range keeps one --size rule across every list command fft has.
	if size != 0 && (size < MinSize || size > MaxSize) {
		return page, exitcode.UsageError{Err: fmt.Errorf(
			"the page size must be between %d and %d, not %d", MinSize, MaxSize, size)}
	}

	res, err := c.Do(ctx, op.Name, func(ctx context.Context) (*http.Response, error) {
		return op.Do(ctx, c.api, after, size)
	})
	if err != nil {
		return page, err
	}
	return decodeListPage(op, res.Body)
}

// ListAll yields every entity, walking the list one page at a time.
//
// It is [SearchAll] for the id-keyed endpoints, and it keeps the same two promises:
// it stops at maxItems ([DefaultMaxItems] unless [MaxItems] says otherwise), and if
// that cut anything off it yields a *[TruncatedError] as its last element. A
// truncated list that does not say it was truncated is a wrong answer that looks
// like a right one.
func ListAll[T any](ctx context.Context, c *Client, op ListOp[T], size int, opts ...AllOption) iter.Seq2[T, error] {
	o := allOptions{maxItems: DefaultMaxItems}
	for _, opt := range opts {
		opt(&o)
	}

	return func(yield func(T, error) bool) {
		var zero T

		if size == 0 {
			size = PageSize
		}

		seen := 0
		after := ""

		for {
			page, err := List(ctx, c, op, after, size)
			if err != nil {
				yield(zero, err)
				return
			}

			for _, item := range page.Items {
				if seen >= o.maxItems {
					yield(zero, &TruncatedError{MaxItems: o.maxItems, Op: op.Name})
					return
				}
				if !yield(item, nil) {
					return
				}
				seen++
			}

			// The end of the list. This envelope has no hasNextPage, so it has to be
			// inferred — and the obvious inference, "a short page is the last page", is
			// not safe on its own.
			//
			// The spec puts no maximum on this endpoint's `size`. A server that silently
			// caps it — answers 100 to a request for 250 — hands back a short *first*
			// page, and "a short page ends the list" would read that as the whole answer:
			// 100 connections, exit 0, no truncation warning. A wrong answer that looks
			// like a right one, which is the one thing this file exists to prevent.
			//
			// So the envelope's own count is the cross-check. It is on every page, and it
			// is the number of matches for this query — so a short page ends the list only
			// once we have seen as many as the API says there are. An empty page ends it
			// unconditionally, which is what guarantees this terminates even if `total` is
			// nonsense.
			if len(page.Items) == 0 || (len(page.Items) < size && seen >= page.Total) {
				return
			}

			next, err := op.ID(page.Items[len(page.Items)-1])
			if err != nil {
				yield(zero, fmt.Errorf("%s: %w", op.Name, err))
				return
			}

			// An id that does not advance would ask for the same page forever. Better to
			// say so than to fetch it until the user gives up.
			if next == after {
				yield(zero, fmt.Errorf("%s: the API keeps returning the page after %q, so there is no way forward", op.Name, after))
				return
			}
			after = next
		}
	}
}

// decodeListPage reads the envelope. As with the searches, the array is looked up by
// the name the [ListOp] gives rather than guessed at, so a field the API grows
// tomorrow cannot quietly become "the entities".
func decodeListPage[T any](op ListOp[T], body []byte) (ListPage[T], error) {
	var page ListPage[T]

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return page, fmt.Errorf("%s: decode the result: %w", op.Name, err)
	}

	items, ok := fields[op.Items]
	if !ok {
		return page, fmt.Errorf("%s: the result has no %q field", op.Name, op.Items)
	}
	if err := json.Unmarshal(items, &page.Items); err != nil {
		return page, fmt.Errorf("%s: decode the %s: %w", op.Name, op.Items, err)
	}

	if total, ok := fields["total"]; ok {
		if err := json.Unmarshal(total, &page.Total); err != nil {
			return page, fmt.Errorf("%s: decode the total: %w", op.Name, err)
		}
	}

	return page, nil
}
