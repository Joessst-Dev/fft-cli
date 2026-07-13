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

// The search API's numbers. The legacy GET list has a *different* default (25):
// the two are not unified into one --limit default, because a user who set
// --size 25 and got 20 would be right to file a bug.
const (
	// MinSize and MaxSize bound `size`. The API rejects anything outside them with
	// an opaque 400, so fft rejects it first, with a sentence.
	MinSize = 1
	MaxSize = 250

	// DefaultSize is what the API uses when `size` is absent.
	DefaultSize = 20

	// PageSize is what [SearchAll] asks for. Well under the maximum, and 5× fewer
	// round trips than the API's own default across a tenant with a few thousand
	// entities.
	PageSize = 100

	// DefaultMaxItems is how far [SearchAll] will follow the cursor before it stops
	// and says so. A runaway cursor must not be able to hang a terminal.
	DefaultMaxItems = 10_000
)

// SearchPayload is the body of every POST /{entity}/search in the API.
//
// This is the leverage point: the shape is identical for facilities, listings,
// stocks, pickjobs and orders — only the query and the sort differ, and those are
// the two type parameters. One generic therefore carries cursor pagination for the
// whole API, and a new entity costs an [Op] and a type alias.
type SearchPayload[Q, S any] struct {
	// Query is the entity's filter. Its zero value marshals to {}, which the API
	// accepts and reads as "everything" — confirmed against the live tenant.
	Query Q `json:"query"`

	// After is the cursor from the previous page's pageInfo.endCursor. [SearchAll]
	// drives it; a caller paging by hand sets it.
	After *string `json:"after,omitempty"`

	// Size is how many items to return: 1–250, 20 if absent.
	Size *int `json:"size,omitempty"`

	// Sort holds *exactly one* element, or is nil. The API's schema says
	// minItems: 1, maxItems: 1, and an empty array comes back as an opaque 400 —
	// so an empty (but non-nil) Sort is refused here, before the request is sent.
	Sort []S `json:"sort,omitempty"`

	// Options asks for the total. Without withTotal:true the response has no
	// `total` field at all — which is why [Page.Total] is a pointer.
	Options *api.SearchOptions `json:"options,omitempty"`
}

// WithTotal asks the API to count the matches, and returns the payload so it can
// be set inline.
func (p SearchPayload[Q, S]) WithTotal() SearchPayload[Q, S] {
	yes := true
	p.Options = &api.SearchOptions{WithTotal: &yes}
	return p
}

// validate refuses a payload the API would reject with a 400 that says nothing.
func (p SearchPayload[Q, S]) validate() error {
	// nil sorts by the server's default; an explicitly empty one is a mistake, and
	// the two are worth distinguishing.
	if p.Sort != nil && len(p.Sort) != 1 {
		return exitcode.UsageError{Err: fmt.Errorf(
			"the API accepts exactly one sort field, not %d", len(p.Sort))}
	}
	if p.Size != nil && (*p.Size < MinSize || *p.Size > MaxSize) {
		return exitcode.UsageError{Err: fmt.Errorf(
			"the page size must be between %d and %d, not %d", MinSize, MaxSize, *p.Size)}
	}
	return nil
}

// Op binds the generic search to one entity's endpoint.
//
// Do is the generated client's method, so the path and the encoding stay in the
// generated code. It takes the body as bytes rather than as an io.Reader because
// [Client.Do] may call it twice: a Reader would be empty the second time.
type Op[T any] struct {
	// Name names the operation in errors ("search the facilities").
	Name string

	// Items is the response field holding the entities: "facilities", "stocks",
	// "listings". It is the one part of the response shape that is not uniform.
	Items string

	// Do issues one page.
	Do func(ctx context.Context, raw api.ClientInterface, body []byte) (*http.Response, error)
}

// Page is one page of search results.
type Page[T any] struct {
	// Items are the entities on this page.
	Items []T

	// PageInfo carries the cursor to the next page, and whether there is one.
	PageInfo api.PageInfo

	// Total is the number of matches — *absent* unless the search asked for it with
	// options.withTotal. A pointer, because "the API did not count" and "the API
	// counted zero" are different answers and a command that renders `Total: 0` for
	// the first one is lying.
	Total *int
}

// Search fetches one page.
func Search[Q, S, T any](ctx context.Context, c *Client, op Op[T], payload SearchPayload[Q, S]) (Page[T], error) {
	var page Page[T]

	if c == nil || op.Do == nil {
		return page, fmt.Errorf("%s: there is no API client", op.Name)
	}
	if err := payload.validate(); err != nil {
		return page, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return page, fmt.Errorf("%s: encode the search: %w", op.Name, err)
	}

	res, err := c.Do(ctx, op.Name, func(ctx context.Context) (*http.Response, error) {
		return op.Do(ctx, c.api, body)
	})
	if err != nil {
		return page, err
	}
	return decodePage[T](op, res.Body)
}

// SearchAll yields every match, following the cursor from page to page.
//
// It stops at maxItems ([DefaultMaxItems] unless [MaxItems] says otherwise) and,
// if that cut anything off, yields a *[TruncatedError] as its last element: a
// truncated list that does not say it was truncated is a wrong answer that looks
// like a right one.
//
// The iteration stops at the first error, which is also yielded. A caller that
// breaks out early simply stops fetching.
func SearchAll[Q, S, T any](ctx context.Context, c *Client, op Op[T], payload SearchPayload[Q, S], opts ...AllOption) iter.Seq2[T, error] {
	o := allOptions{maxItems: DefaultMaxItems}
	for _, opt := range opts {
		opt(&o)
	}

	return func(yield func(T, error) bool) {
		var zero T

		if payload.Size == nil {
			size := PageSize
			payload.Size = &size
		}

		seen := 0
		cursor := ""

		for {
			page, err := Search(ctx, c, op, payload)
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

			if !page.PageInfo.HasNextPage {
				return
			}

			// A next page fft cannot address, or one it has already read, is a loop.
			// Better to say so than to fetch the same page until the user gives up.
			next := page.PageInfo.EndCursor
			if next == "" || next == cursor {
				yield(zero, fmt.Errorf("%s: the API reports another page but gives no new cursor to it", op.Name))
				return
			}

			cursor = next
			payload.After = &cursor
		}
	}
}

// AllOption configures [SearchAll].
type AllOption func(*allOptions)

type allOptions struct{ maxItems int }

// MaxItems caps how many items [SearchAll] yields. A value below 1 is ignored.
func MaxItems(n int) AllOption {
	return func(o *allOptions) {
		if n > 0 {
			o.maxItems = n
		}
	}
}

// TruncatedError says that a search matched more than [SearchAll] was allowed to
// yield. It is not a failure of the request — the items already yielded are real —
// it is the reason there are no more.
type TruncatedError struct {
	MaxItems int
	Op       string
}

func (e *TruncatedError) Error() string {
	return fmt.Sprintf("there are more results: %s stopped after %d items", e.Op, e.MaxItems)
}

// Hint tells the user how to see the rest.
func (e *TruncatedError) Hint() string {
	return "Narrow the search with a filter, or raise --max-items."
}

// decodePage reads the paginated envelope.
//
// The envelope is uniform but for the name of the array: facilities, stocks,
// listings. So the field is looked up by the name the [Op] gives, rather than
// guessing that it is the one key that is not pageInfo — a rule a future field on
// the response would quietly break.
func decodePage[T any](op Op[T], body []byte) (Page[T], error) {
	var page Page[T]

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return page, fmt.Errorf("%s: decode the search result: %w", op.Name, err)
	}

	items, ok := fields[op.Items]
	if !ok {
		return page, fmt.Errorf("%s: the search result has no %q field", op.Name, op.Items)
	}
	if err := json.Unmarshal(items, &page.Items); err != nil {
		return page, fmt.Errorf("%s: decode the %s: %w", op.Name, op.Items, err)
	}

	if info, ok := fields["pageInfo"]; ok {
		if err := json.Unmarshal(info, &page.PageInfo); err != nil {
			return page, fmt.Errorf("%s: decode the page info: %w", op.Name, err)
		}
	}

	total, err := decodeTotal(fields["total"])
	if err != nil {
		return page, fmt.Errorf("%s: decode the total: %w", op.Name, err)
	}
	page.Total = total

	return page, nil
}

// decodeTotal distinguishes an absent total from a total of zero. The API omits
// the field entirely unless the search asked for it.
func decodeTotal(raw json.RawMessage) (*int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, err
	}
	// The schema types it as a number rather than an integer, so it is read as one
	// and narrowed here, where a fractional count would be a bug worth reporting.
	i, err := n.Int64()
	if err != nil {
		return nil, err
	}

	total := int(i)
	return &total, nil
}
