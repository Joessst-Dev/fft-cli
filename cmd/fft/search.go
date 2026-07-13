package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// listView is how one entity's list command renders a page.
//
// Every POST /{entity}/search in the API has the same envelope, so the paging,
// the totals, the truncation notice and the output contract are written once
// here; an entity supplies only its noun and its table.
type listView struct {
	// Noun names the entities, plural: "facilities", "listings", "stocks". It is
	// what an empty result and a truncation notice are phrased with.
	Noun string

	// Rows builds the table from the API's own JSON. An entity fft cannot parse is
	// a bug worth reporting, not a row worth skipping — so this may fail.
	Rows func(style output.Style, items []json.RawMessage) (output.Rows, error)
}

// pageFlags are the pagination flags every search-backed command shares.
type pageFlags struct {
	size     int
	all      bool
	total    bool
	maxItems int
}

// register installs the flags. noun is the plural the help text is phrased with.
func (p *pageFlags) register(f *pflag.FlagSet, noun string) {
	f.IntVar(&p.size, "size", 0,
		fmt.Sprintf("%s per page, %d–%d (default %d)",
			strings.ToUpper(noun[:1])+noun[1:], client.MinSize, client.MaxSize, client.DefaultSize))
	f.BoolVar(&p.all, "all", false, "Follow the cursor and return every match, not just the first page")
	f.BoolVar(&p.total, "total", false, "Also count the matches, and report the total on stderr")
	f.IntVar(&p.maxItems, "max-items", client.DefaultMaxItems,
		fmt.Sprintf("With --all, stop after this many %s", noun))
}

// apply folds the flags into the search payload.
//
// sized reports whether --size was given at all, which is not the same as its
// value being non-zero: --size 0 is out of range, and letting it fall through as
// "unset" would silently give the user the API's default of 20 while --size 500
// is refused. Both ends of the range have to fail the same way.
func applyPage[Q, S any](p pageFlags, payload client.SearchPayload[Q, S], sized bool) client.SearchPayload[Q, S] {
	if sized {
		size := p.size
		payload.Size = &size
	}

	// withTotal is only asked for on a single page. Under --all the count is the
	// number of entities fft actually yielded, so asking the API to count them
	// again on every page of the cursor would be paying for an answer we already
	// have.
	if p.total && !p.all {
		payload = payload.WithTotal()
	}
	return payload
}

// checkMaxItems refuses a --max-items that cannot be honoured.
//
// Zero would page the API and print nothing, which looks exactly like an empty
// tenant. And --max-items without --all does nothing at all: it caps the cursor,
// and without --all there is no cursor to cap. A flag that silently does nothing is
// discovered weeks later by someone whose cap has never once applied, so it is a
// usage error instead.
func checkMaxItems(cmd *cobra.Command, page pageFlags) error {
	if !cmd.Flags().Changed("max-items") {
		return nil
	}

	switch {
	case page.maxItems < 1:
		return exitcode.UsageError{Err: fmt.Errorf(
			"--max-items must be at least 1, and %d is not", page.maxItems)}

	case !page.all:
		return exitcode.UsageError{Err: fmt.Errorf(
			"--max-items only means something with --all: it caps how far the cursor is followed, and without --all there is one page. Use --size to change the size of that page")}
	}
	return nil
}

// buildFn produces the search payload once the client exists.
//
// Most commands know their query before they have a client and use [staticQuery].
// `fft listing list` does not: the listing search can only filter on facilityRef,
// so a --facility given as a tenantFacilityId has to be *resolved* to a platform
// id first — which takes a request, which takes a client.
type buildFn[Q, S any] func(ctx context.Context, c *client.Client) (client.SearchPayload[Q, S], error)

// staticQuery is the buildFn of a command whose query needs nothing from the API.
func staticQuery[Q, S any](payload client.SearchPayload[Q, S]) buildFn[Q, S] {
	return func(context.Context, *client.Client) (client.SearchPayload[Q, S], error) {
		return payload, nil
	}
}

// runSearch is the body of every `list` and every `search`: they differ only in
// where the query came from.
func runSearch[Q, S any](cmd *cobra.Command, deps *Deps, op client.Op[json.RawMessage],
	build buildFn[Q, S], page pageFlags, view listView,
) error {
	if err := checkMaxItems(cmd, page); err != nil {
		return err
	}

	c, err := tenantClient(deps)
	if err != nil {
		return err
	}

	ctx, cancel := deps.Context(cmd)
	defer cancel()

	payload, err := build(ctx, c)
	if err != nil {
		return err
	}
	payload = applyPage(page, payload, cmd.Flags().Changed("size"))

	if page.all {
		return renderAll(ctx, deps, c, op, payload, page, view)
	}
	return renderPage(ctx, deps, c, op, payload, view)
}

func renderPage[Q, S any](ctx context.Context, deps *Deps, c *client.Client, op client.Op[json.RawMessage],
	payload client.SearchPayload[Q, S], view listView,
) error {
	result, err := client.Search(ctx, c, op, payload)
	if err != nil {
		return err
	}

	// Absent is not zero. Without options.withTotal the API sends no total at all,
	// and printing "Total: 0" for a page of 20 entities would be a lie fft told
	// itself first.
	if result.Total != nil {
		deps.Printer.Notef("Total: %d", *result.Total)
	}
	if result.PageInfo.HasNextPage && deps.Printer.Format() == output.Table {
		deps.Printer.Notef("There are more %s. Pass --all to fetch them.", view.Noun)
	}

	return renderList(deps, view, result.Items)
}

func renderAll[Q, S any](ctx context.Context, deps *Deps, c *client.Client, op client.Op[json.RawMessage],
	payload client.SearchPayload[Q, S], page pageFlags, view listView,
) error {
	var (
		items     []json.RawMessage
		truncated *client.TruncatedError
	)

	for item, err := range client.SearchAll(ctx, c, op, payload, client.MaxItems(page.maxItems)) {
		switch {
		// A truncation is not a failure: every entity already yielded is real. It
		// ends the iteration and is reported — loudly, and on stderr — because a
		// truncated list that does not say so is a wrong answer that looks right.
		case errors.As(err, &truncated):
		case err != nil:
			return err
		default:
			items = append(items, item)
		}
		if truncated != nil {
			break
		}
	}

	if truncated != nil {
		deps.Printer.Warnf("%s. %s", truncated, truncated.Hint())
	}

	// --all read every match, so the count *is* the total — but only if it reached
	// the end. A truncated run knows how many it fetched and nothing about how many
	// it did not, and reporting the former as the latter would be a wrong number
	// stated with confidence.
	if page.total && truncated == nil {
		deps.Printer.Notef("Total: %d", len(items))
	}

	return renderList(deps, view, items)
}

// renderList is where the output contract is kept: the table is fft's view model,
// -o json is the API's own bytes, and an empty result leaves stdout empty while
// saying so on stderr.
func renderList(deps *Deps, view listView, items []json.RawMessage) error {
	if len(items) == 0 {
		return deps.Printer.Empty(view.Noun)
	}

	rows, err := view.Rows(deps.Printer.Style(), items)
	if err != nil {
		return err
	}

	// []json.RawMessage marshals to the array of entities exactly as the API wrote
	// them — no field is reshaped, and none is dropped.
	raw, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("encode the %s: %w", view.Noun, err)
	}

	return deps.Printer.RenderRaw(rows, raw)
}

// searchPayload decodes a `search --file` body, and refuses one the API would not
// understand.
//
// DisallowUnknownFields is the point. A query is a deep tree of optional filters,
// and the API's answer to `{"statuz": …}` is not an error but a 200 listing
// everything — a filter that silently does not filter. Checking the body against
// the generated schema turns that into "unknown field statuz", before a byte goes
// over the wire.
func searchPayload[Q, S any](deps *Deps, path, entity string) (client.SearchPayload[Q, S], error) {
	var payload client.SearchPayload[Q, S]

	raw, err := readBody(deps, path)
	if err != nil {
		return payload, err
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&payload); err != nil {
		return payload, exitcode.UsageError{Err: fmt.Errorf("%s is not a valid %s search: %w", path, entity, err)}
	}
	return payload, nil
}

// parseSort turns --sort field:asc into the API's sort object.
//
// The API's sort is a *struct* with exactly one field set, not a string, so
// "name:desc" becomes {Name: "DESC"} — and it accepts exactly one field, which is
// why every caller returns a slice of at most one. set assigns the direction to
// the named field, or reports that the field cannot be sorted on.
func parseSort[S any](v string, fields []string, set func(*S, string, string) bool) ([]S, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}

	name, order, ok := strings.Cut(v, ":")
	if !ok {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			"--sort %q is not field:asc or field:desc: for example --sort %s:asc", v, fields[0])}
	}

	dir := strings.ToUpper(strings.TrimSpace(order))
	if dir != "ASC" && dir != "DESC" {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			"--sort %q: the direction must be asc or desc, not %q", v, order)}
	}

	var sort S
	if !set(&sort, strings.ToLower(strings.TrimSpace(name)), dir) {
		return nil, exitcode.UsageError{Err: fmt.Errorf("cannot sort by %q: want one of %s",
			name, strings.Join(fields, ", "))}
	}
	return []S{sort}, nil
}
