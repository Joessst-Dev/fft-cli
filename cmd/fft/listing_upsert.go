package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const listingUpsertLong = `Upsert listings across facilities, from one file.

This is PUT /api/listings: unlike 'fft listing set' it is tenant-wide, so one
file can write the same article into fifty stores, and it reports per item
rather than failing as a block.

  fft listing upsert --example > bulk.json
  $EDITOR bulk.json
  fft listing upsert --file bulk.json

Every entry names its targets with a targetingStrategy:

  SINGLE_FACILITY  "facility": { "tenantFacilityId": "BER-01" }
  MULTI_SELECTOR   "selector": [ { "facility": { "tenantFacilityId": "BER-01" } },
                                 { "facility": { "facilityRef": "8f14e45f-..." } } ]

The API caps one request at 25 (entries × selectors). A real catalog import is
far larger than that, so fft SPLITS the file into as many requests as it takes
and reports the outcome of every entry — you write the file you mean, and fft
deals with the limit. Nothing is dropped: every entry of the file is sent, and
every entry appears in the result table.

Chunks are sent one after another, and a chunk that fails does not stop the
ones after it. If some entries land and others do not, the command exits 8 and
the FAILED rows say why — re-send only those.`

// maxBulkBudget is the API's hard limit on one bulk-upsert request:
//
//	"Number of entries times selectors must not exceed 25."  (swagger:47286)
//
// Nothing in the schema enforces it — it lives in a description — so the API's
// answer to a 26-pair request is a 400. A naive implementation therefore works
// on the example in the docs and fails on the first real catalog import.
const maxBulkBudget = 25

// listingUpsertExample shows both targeting strategies, because the discriminator
// is the field users get wrong.
// listingUpsertExample is hand-written, not synthesized: it has to show BOTH
// targeting strategies (SINGLE_FACILITY and MULTI_SELECTOR) and the tenantFacilityId
// spelling that actually resolves, none of which the schema says. The synthesized
// body is valid but names a facility by the placeholder "string". See
// stockCreateExample.
const listingUpsertExample = `{
  "listings": [
    {
      "targetingStrategy": "SINGLE_FACILITY",
      "tenantArticleId": "4711",
      "title": "Adidas Superstar",
      "status": "ACTIVE",
      "price": 89.95,
      "facility": { "tenantFacilityId": "BER-01" }
    },
    {
      "targetingStrategy": "MULTI_SELECTOR",
      "tenantArticleId": "4712",
      "title": "Adidas Gazelle",
      "status": "ACTIVE",
      "price": 79.95,
      "selector": [
        { "facility": { "tenantFacilityId": "BER-01" } },
        { "facility": { "tenantFacilityId": "HAM-01" } }
      ]
    }
  ]
}
`

// The two targeting strategies (swagger:47290). They are the oneOf's
// discriminator, and an entry that names neither is a 400 that does not say which
// of the two shapes it failed to match.
const (
	targetSingleFacility = "SINGLE_FACILITY"
	targetMultiSelector  = "MULTI_SELECTOR"
)

func newListingUpsertCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
	)

	cmd := &cobra.Command{
		Use:   "upsert --file <file>",
		Short: "Upsert listings across facilities (chunked, per-item result)",
		Long:  listingUpsertLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "bulkUpsertListings"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			if example {
				_, err := fmt.Fprint(cmd.OutOrStdout(), listingUpsertExample)
				return err
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}

			raw, err := readBody(deps, file)
			if err != nil {
				return err
			}

			entries, err := parseUpsertEntries(raw, file)
			if err != nil {
				return err
			}

			chunks, err := chunkUpsert(entries, file)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			if len(chunks) > 1 {
				deps.Printer.Notef("Sending %d listings in %d requests (the API accepts %d entry×facility pairs per request).",
					len(entries), len(chunks), maxBulkBudget)
			}

			results, err := sendUpsertChunks(ctx, deps, c, chunks)

			// What is known is printed even when the run was cut short, because the
			// listings it names really were written and this table is the only record of
			// them. The command still exits on the cause — 4 for an expired token, not 8
			// for a partial write.
			if err != nil {
				reportAborted(deps, "listings", results)
				return err
			}

			return renderBulk(deps, "listings", results)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", `JSON file holding {"listings": [...]} ('-' for stdin)`)
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}

// upsertEntry is one listing of the bulk payload, kept as the user wrote it.
//
// Raw is the entry's own bytes and is what actually gets sent: an entry has three
// dozen optional fields and fft models four of them, so re-encoding a decoded form
// would silently drop everything else the user carefully wrote.
type upsertEntry struct {
	Raw json.RawMessage

	// TenantArticleID names the entry in the result table, and is what the API's
	// answer is reconciled against.
	TenantArticleID string

	// Cost is how much of the 25-pair budget this entry spends: one per facility it
	// targets.
	Cost int
}

// upsertShape is the handful of fields fft has to understand in order to chunk.
// Everything else passes through untouched.
type upsertShape struct {
	TargetingStrategy string            `json:"targetingStrategy"`
	TenantArticleID   string            `json:"tenantArticleId"`
	Facility          json.RawMessage   `json:"facility"`
	Selector          []json.RawMessage `json:"selector"`
}

// parseUpsertEntries reads the file and refuses what the API would refuse, but
// with a message that names the entry.
func parseUpsertEntries(raw []byte, path string) ([]upsertEntry, error) {
	var body struct {
		Listings []json.RawMessage `json:"listings"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			`%s is not a bulk upsert: it must be an object like {"listings": [...]}: %w`, path, err)}
	}
	if len(body.Listings) == 0 {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			`%s holds no listings — run 'fft listing upsert --example' for a body to start from`, path)}
	}

	entries := make([]upsertEntry, 0, len(body.Listings))
	for i, item := range body.Listings {
		entry, err := parseUpsertEntry(item, path, i+1, len(body.Listings))
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func parseUpsertEntry(raw json.RawMessage, path string, n, total int) (upsertEntry, error) {
	where := fmt.Sprintf("%s: listing %d of %d", path, n, total)

	var shape upsertShape
	if err := json.Unmarshal(raw, &shape); err != nil {
		return upsertEntry{}, exitcode.UsageError{Err: fmt.Errorf("%s is not an object: %w", where, err)}
	}
	if shape.TenantArticleID == "" {
		return upsertEntry{}, exitcode.UsageError{Err: fmt.Errorf(
			"%s has no tenantArticleId, and the API requires one", where)}
	}

	entry := upsertEntry{Raw: raw, TenantArticleID: shape.TenantArticleID}

	// The cost of an entry is the number of (listing, facility) pairs it asks the
	// API to write. That is what the budget is spent on, and it is the difference
	// between the two strategies.
	switch shape.TargetingStrategy {
	case targetSingleFacility:
		if len(shape.Facility) == 0 {
			return upsertEntry{}, exitcode.UsageError{Err: fmt.Errorf(
				`%s is %s but has no "facility"`, where, targetSingleFacility)}
		}
		entry.Cost = 1

	case targetMultiSelector:
		if len(shape.Selector) == 0 {
			return upsertEntry{}, exitcode.UsageError{Err: fmt.Errorf(
				`%s is %s but has no "selector": it needs at least one`, where, targetMultiSelector)}
		}
		entry.Cost = len(shape.Selector)

	case "":
		return upsertEntry{}, exitcode.UsageError{Err: fmt.Errorf(
			`%s has no "targetingStrategy": every entry must say whether it targets one facility (%s) or several (%s)`,
			where, targetSingleFacility, targetMultiSelector)}

	default:
		return upsertEntry{}, exitcode.UsageError{Err: fmt.Errorf(
			"%s has an unknown targetingStrategy %q: want %s or %s",
			where, shape.TargetingStrategy, targetSingleFacility, targetMultiSelector)}
	}

	return entry, nil
}

// chunkUpsert splits the entries into requests the API will accept.
//
// # What the budget actually is
//
// The constraint reads "Number of entries times selectors must not exceed 25".
// An entry that targets one facility costs 1; one that targets five costs 5. A
// chunk is therefore kept within `len(chunk) × the largest cost in it ≤ 25`,
// which is the *literal* product the sentence describes and is never larger than
// the sum of the costs — so it satisfies the stricter of the two readings the
// sentence admits, and a request fft sends cannot be rejected for exceeding it.
//
// A single entry that on its own exceeds the budget cannot be chunked at all: an
// entry is atomic, fft will not split one listing's selectors across two requests
// and half-write it. That is a usage error naming the entry, not a 400 an hour
// into an import.
func chunkUpsert(entries []upsertEntry, path string) ([][]upsertEntry, error) {
	var (
		chunks  [][]upsertEntry
		current []upsertEntry
		maxCost int
	)

	flush := func() {
		if len(current) > 0 {
			chunks = append(chunks, current)
			current, maxCost = nil, 0
		}
	}

	for i, entry := range entries {
		if entry.Cost > maxBulkBudget {
			return nil, exitcode.UsageError{Err: fmt.Errorf(
				"%s: listing %d of %d (%s) targets %d facilities, and the API accepts at most %d per request — split it into several entries",
				path, i+1, len(entries), entry.TenantArticleID, entry.Cost, maxBulkBudget)}
		}

		// Would adding this entry break the budget? If so, the chunk is closed first.
		cost := max(maxCost, entry.Cost)
		if (len(current)+1)*cost > maxBulkBudget {
			flush()
			cost = entry.Cost
		}

		current = append(current, entry)
		maxCost = cost
	}
	flush()

	return chunks, nil
}

// sendUpsertChunks sends each chunk and collects what became of every entry.
//
// A chunk that fails does not stop the ones after it: an import of 5,000 listings
// should not be abandoned because one article has a bad category ref. The entries
// of a failed chunk are reported FAILED with the API's own message, and the exit
// code says the write was partial — which is the truth, and is the thing a script
// most needs to know.
//
// A transport failure or a cancelled context *does* stop it: those are not "this
// entry was bad", they are "fft cannot talk to the API", and grinding through
// another forty requests to say so forty more times helps nobody.
func sendUpsertChunks(ctx context.Context, deps *Deps, c *client.Client, chunks [][]upsertEntry) ([]bulkResult, error) {
	var results []bulkResult

	for i, chunk := range chunks {
		op := fmt.Sprintf("upsert listings (request %d of %d)", i+1, len(chunks))

		body, err := encodeUpsertChunk(chunk)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}

		raw, err := sendDoc(ctx, c, op, body,
			func(ctx context.Context, body io.Reader) (*http.Response, error) {
				return c.API().BulkUpsertListingsWithBody(ctx, contentTypeJSON, body)
			})
		if err != nil {
			// Only a rejection of *this chunk's entries* is a per-item failure. A dead
			// connection, an expired token or a rate limit is not about the entries, and
			// the next request would fail the same way — see [chunkRejected].
			//
			// The results so far are handed back *with* the error, not discarded. A token
			// that expires on request 7 of 40 leaves six requests' worth of listings
			// written, and a user who is not told which ones can only re-run the whole
			// file.
			if ctx.Err() != nil || !chunkRejected(err) {
				return results, err
			}

			deps.Printer.Warnf("Request %d of %d failed: %v", i+1, len(chunks), err)
			results = append(results, failedChunk(chunk, err.Error())...)
			continue
		}

		results = append(results, upsertChunkResults(chunk, raw)...)
	}

	return results, nil
}

// encodeUpsertChunk rebuilds the payload from the entries' own bytes, so that
// every field the user wrote survives the round trip through fft.
func encodeUpsertChunk(chunk []upsertEntry) ([]byte, error) {
	listings := make([]json.RawMessage, 0, len(chunk))
	for _, entry := range chunk {
		listings = append(listings, entry.Raw)
	}

	body, err := json.Marshal(map[string]any{"listings": listings})
	if err != nil {
		return nil, fmt.Errorf("encode the request: %w", err)
	}
	return body, nil
}

// failedChunk reports every entry of a rejected chunk as failed. A chunk is atomic
// to the API: if it was rejected, none of its entries landed, and re-sending them
// is the right thing to do.
func failedChunk(chunk []upsertEntry, reason string) []bulkResult {
	return chunkResults(chunk, bulkFailed, reason)
}

func chunkResults(chunk []upsertEntry, status, reason string) []bulkResult {
	results := make([]bulkResult, 0, len(chunk))
	for _, entry := range chunk {
		results = append(results, bulkResult{Key: entry.TenantArticleID, Status: status, Reason: reason})
	}
	return results
}

// listingUpsertSuccess is one entry of the API's answer (swagger:47322).
type listingUpsertSuccess struct {
	Status string `json:"status"`
	Result struct {
		TenantArticleID string `json:"tenantArticleId"`
		FacilityID      string `json:"facilityId"`
	} `json:"result"`
}

// upsertChunkResults reconciles what fft sent against what the API reported.
//
// The response carries only the listings that were *successfully* upserted
// (swagger:47306) — there is no per-item failure channel. So an entry fft sent
// whose tenantArticleId is absent from the answer is an entry the API silently did
// not write, and reporting the chunk as a success would hide exactly the thing the
// user needs to know. It is reported FAILED, and the exit code says so.
func upsertChunkResults(chunk []upsertEntry, raw []byte) []bulkResult {
	var answer struct {
		Listings []listingUpsertSuccess `json:"listings"`
	}

	// An empty 2xx body is a legitimate answer, and json.Unmarshal calls it
	// "unexpected end of JSON input". Either way the listings have landed and only
	// their description is missing — which is UNKNOWN, not FAILED. (A listing upsert
	// is keyed on tenantArticleId and so re-sending one is harmless, unlike a stock
	// create; but the report should still say what is true rather than what is safe.)
	if err := json.Unmarshal(raw, &answer); err != nil {
		return chunkResults(chunk, bulkUnknown,
			"the API accepted the request but its answer could not be read")
	}

	reported := make(map[string]bool, len(answer.Listings))
	results := make([]bulkResult, 0, len(answer.Listings)+len(chunk))

	for _, item := range answer.Listings {
		reported[item.Result.TenantArticleID] = true
		results = append(results, bulkResult{
			Key:      item.Result.TenantArticleID,
			Facility: item.Result.FacilityID,
			Status:   item.Status,
		})
	}

	for _, entry := range chunk {
		if !reported[entry.TenantArticleID] {
			results = append(results, bulkResult{
				Key:    entry.TenantArticleID,
				Status: bulkFailed,
				Reason: "the API accepted the request but reported no result for this listing",
			})
		}
	}

	return results
}
