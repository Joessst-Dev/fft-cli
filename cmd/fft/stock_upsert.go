package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const stockUpsertLong = `Create and update many stocks at once, from one file.

This is PUT /api/stocks. It is how a nightly inventory sync writes: one file,
one pass, a result per stock.

  fft stock upsert --example > stocks.json
  $EDITOR stocks.json
  fft stock upsert --file stocks.json

An entry that carries an "id" updates that stock. An entry without one creates a
stock, and must name its article, its quantity and exactly one facility — the
same rule 'fft stock create' enforces, for the same reason.

The API accepts 500 stocks per request, so fft SPLITS a larger file into as many
requests as it takes. A chunk that fails does not stop the ones after it: the
entries of a failed chunk are reported FAILED with the API's own message, and
the command exits 8 — some of your stocks landed and some did not, and that is
worth an exit code of its own.

Do not name the same stock twice in one file. The API rejects the whole batch if
you do, and fft says so before sending.`

// maxStockUpsert is the API's cap on one request (swagger:79018, maxItems: 500).
// Unlike the listing bulk upsert, the limit is in the schema — but the API's answer
// to 501 stocks is still a 400, and a nightly sync of a real catalog is far more
// than 500 rows.
const maxStockUpsert = 500

// stockUpsertExample is hand-written, not synthesized: the schema marks the facility
// selector optional and the API does not. See stockCreateExample for the full reason.
const stockUpsertExample = `{
  "stocks": [
    {
      "tenantArticleId": "4711",
      "value": 12,
      "facility": { "tenantFacilityId": "BER-01" },
      "locationRef": "shelf-a-12"
    },
    {
      "id": "019c41f1-8f14-7000-9575-25a1b5c8b3a1",
      "value": 0
    }
  ]
}
`

func newStockUpsertCmd(deps *Deps) *cobra.Command {
	var (
		file    string
		example bool
	)

	cmd := &cobra.Command{
		Use:   "upsert --file <file>",
		Short: "Create and update many stocks at once (chunked, per-item result)",
		Long:  stockUpsertLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "upsertStocks"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			if example {
				_, err := fmt.Fprint(cmd.OutOrStdout(), stockUpsertExample)
				return err
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}

			raw, err := readBody(deps, file)
			if err != nil {
				return err
			}

			entries, err := parseStockEntries(raw, file)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			chunks := chunkStocks(entries, maxStockUpsert)
			if len(chunks) > 1 {
				deps.Printer.Notef("Sending %d stocks in %d requests (the API accepts %d per request).",
					len(entries), len(chunks), maxStockUpsert)
			}

			results, err := sendStockChunks(ctx, deps, c, chunks)

			// What is known is printed even when the run was cut short: for a create,
			// re-running the whole file duplicates every stock that already landed, so
			// this table is what makes a safe retry possible at all.
			if err != nil {
				reportAborted(deps, "stocks", results)
				return err
			}

			return renderBulk(deps, "stocks", results)
		},
	}

	f := cmd.Flags()
	f.StringVar(&file, "file", "", `JSON file holding {"stocks": [...]} ('-' for stdin)`)
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}

// stockEntry is one stock of the bulk payload, kept as the user wrote it.
//
// Raw is the entry's own bytes and is what gets sent: a stock has two dozen
// optional fields and fft models three of them, so re-encoding a decoded form
// would drop everything else the user wrote.
type stockEntry struct {
	Raw json.RawMessage

	// Key is how the entry is named in the result table and matched against the
	// answer: the platform id for an update, the article id for a create.
	Key string
}

// parseStockEntries reads the file and refuses what the API would refuse — but
// with a message that names the entry rather than the batch.
func parseStockEntries(raw []byte, path string) ([]stockEntry, error) {
	var body struct {
		Stocks []json.RawMessage `json:"stocks"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			`%s is not a stock upsert: it must be an object like {"stocks": [...]}: %w`, path, err)}
	}
	if len(body.Stocks) == 0 {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			`%s holds no stocks — run 'fft stock upsert --example' for a body to start from`, path)}
	}

	entries := make([]stockEntry, 0, len(body.Stocks))
	seen := make(map[string]int, len(body.Stocks))

	for i, item := range body.Stocks {
		where := fmt.Sprintf("%s: stock %d of %d", path, i+1, len(body.Stocks))

		doc, err := decodeDoc(item, where)
		if err != nil {
			return nil, exitcode.UsageError{Err: err}
		}

		id := docString(doc, "id")
		entry := stockEntry{Raw: item, Key: id}

		if id == "" {
			// No id means a create, and a create has to say what it is stock *of* and
			// where. The facility rule is the same one 'fft stock create' enforces: the
			// schema requires none of the three fields, so the server's complaint about
			// a body with zero or two of them does not name the field.
			article := docString(doc, "tenantArticleId")
			if article == "" {
				return nil, exitcode.UsageError{Err: fmt.Errorf(
					`%s has neither an "id" (to update an existing stock) nor a "tenantArticleId" (to create one)`, where)}
			}
			if err := checkStockFacility(doc, where); err != nil {
				return nil, err
			}
			entry.Key = article
		}

		// The API rejects the *whole batch* if a stock is named twice, so finding it
		// here saves the user a 400 whose message is about a batch rather than a line.
		if id != "" {
			if first, dup := seen[id]; dup {
				return nil, exitcode.UsageError{Err: fmt.Errorf(
					"%s: stock %s appears twice, at entries %d and %d — the API rejects the whole batch when it does",
					path, id, first, i+1)}
			}
			seen[id] = i + 1
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// chunkStocks splits the entries into requests the API will accept. The budget is
// a plain count here — unlike the listing upsert, where an entry can target many
// facilities and so costs more than one.
func chunkStocks(entries []stockEntry, size int) [][]stockEntry {
	return slices.Collect(slices.Chunk(entries, size))
}

// sendStockChunks sends each chunk and collects what became of every entry.
//
// As with the listing upsert, a chunk the *server* rejected is reported per entry
// and the run continues; a transport failure or a cancelled context stops it,
// because those say nothing about the entries and the next request would fail the
// same way.
func sendStockChunks(ctx context.Context, deps *Deps, c *client.Client, chunks [][]stockEntry) ([]bulkResult, error) {
	var results []bulkResult

	for i, chunk := range chunks {
		op := fmt.Sprintf("upsert stocks (request %d of %d)", i+1, len(chunks))

		body, err := encodeStockChunk(chunk)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}

		raw, err := sendDoc(ctx, c, op, body,
			func(ctx context.Context, body io.Reader) (*http.Response, error) {
				return c.API().UpsertStocksWithBody(ctx, contentTypeJSON, body)
			})
		if err != nil {
			// Only a rejection of *this chunk's entries* is a per-item failure — see
			// [chunkRejected]. An expired token halfway through a nightly sync must not
			// mark five hundred stocks FAILED with "the token is not valid".
			//
			// The results so far come back *with* the error rather than being thrown
			// away: the stocks already written are real, and for creates a user who does
			// not know which they were cannot safely re-run the file at all.
			if ctx.Err() != nil || !chunkRejected(err) {
				return results, err
			}

			deps.Printer.Warnf("Request %d of %d failed: %v", i+1, len(chunks), err)
			results = append(results, failedStockChunk(chunk, err.Error())...)
			continue
		}

		results = append(results, stockChunkResults(chunk, raw)...)
	}

	return results, nil
}

func encodeStockChunk(chunk []stockEntry) ([]byte, error) {
	stocks := make([]json.RawMessage, 0, len(chunk))
	for _, entry := range chunk {
		stocks = append(stocks, entry.Raw)
	}

	body, err := json.Marshal(map[string]any{"stocks": stocks})
	if err != nil {
		return nil, fmt.Errorf("encode the request: %w", err)
	}
	return body, nil
}

// stockUpsertResult is one entry of the API's answer (swagger:79210): the stock as
// it now stands, and whether it was created or updated.
type stockUpsertResult struct {
	Status string `json:"status"`
	Stock  struct {
		ID              string `json:"id"`
		TenantArticleID string `json:"tenantArticleId"`
		FacilityRef     string `json:"facilityRef"`
	} `json:"stock"`
}

// stockChunkResults reconciles what fft sent against what the API reported.
//
// The answer is an array of results for the stocks that were written; there is no
// per-item failure channel. So an entry fft sent that the answer does not account
// for is one the API silently did not write, and calling the chunk a success would
// hide exactly the thing the user needs to know.
//
// # Why the reconciliation counts rather than sets
//
// The obvious implementation — a set of the keys the answer mentioned — collapses
// entries that share a key, and two entries legitimately can. The same article at
// two locations is two creates both keyed on its tenantArticleId; an update of
// stock X and a create of the same article are two entries the answer describes
// with the same tenantArticleId. In a set, one answer would mark both entries
// reported, and a stock the API silently dropped would leave no trace at all.
//
// So each answered result decrements a count. Two sent and one answered produces
// exactly one FAILED row, which is the truth.
func stockChunkResults(chunk []stockEntry, raw []byte) []bulkResult {
	var answer []stockUpsertResult

	// An empty 2xx body is a legitimate answer, and json.Unmarshal calls it
	// "unexpected end of JSON input". Either way the write has landed: what fft does
	// not have is a description of it, and UNKNOWN says exactly that.
	if err := json.Unmarshal(raw, &answer); err != nil {
		return unknownStockChunk(chunk)
	}

	// The ids fft actually sent. An answer's id is only usable as a key if it is one
	// of them: a *created* stock comes back under an id fft has never seen, and
	// filing it under that id would leave the entry that created it looking dropped.
	sent := make(map[string]bool, len(chunk))
	for _, entry := range chunk {
		sent[entry.Key] = true
	}

	reported := make(map[string]int, len(answer))
	results := make([]bulkResult, 0, len(answer)+len(chunk))

	for _, item := range answer {
		key := item.Stock.ID
		if key == "" || !sent[key] {
			key = item.Stock.TenantArticleID
		}

		reported[key]++
		results = append(results, bulkResult{
			Key:      key,
			Facility: item.Stock.FacilityRef,
			Status:   item.Status,
		})
	}

	for _, entry := range chunk {
		if reported[entry.Key] > 0 {
			reported[entry.Key]--
			continue
		}
		results = append(results, bulkResult{
			Key:    entry.Key,
			Status: bulkFailed,
			Reason: "the API accepted the request but reported no result for this stock",
		})
	}

	return results
}

// failedStockChunk reports every entry of a rejected chunk as failed. A chunk is
// atomic to the API, so if it was rejected then none of its entries landed — and
// re-sending them is exactly the right thing to do.
func failedStockChunk(chunk []stockEntry, reason string) []bulkResult {
	return stockResults(chunk, bulkFailed, reason)
}

// unknownStockChunk reports a chunk the API accepted and then described
// unreadably.
//
// These are *not* FAILED. The write landed, and FAILED means "re-send this" — but
// a stock entry with no id is a create, so re-sending it makes a second stock. The
// honest report is that fft does not know, and the honest advice is to go and look.
func unknownStockChunk(chunk []stockEntry) []bulkResult {
	return stockResults(chunk, bulkUnknown,
		"the API accepted the request but its answer could not be read")
}

func stockResults(chunk []stockEntry, status, reason string) []bulkResult {
	results := make([]bulkResult, 0, len(chunk))
	for _, entry := range chunk {
		results = append(results, bulkResult{Key: entry.Key, Status: status, Reason: reason})
	}
	return results
}
