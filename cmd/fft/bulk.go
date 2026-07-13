package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// The outcomes a bulk write reports per item.
//
// CREATED, UPDATED and UNCHANGED are the API's own (swagger:31400). FAILED is the
// API's too for the listing PUT, and is fft's for everything else: when a chunked
// upsert has a chunk rejected, the entries of that chunk did not land.
//
// UNKNOWN is fft's alone, and it is the important one. It means the API *accepted*
// the request and then said something fft could not read — so the write has landed
// and fft cannot say what it did. Calling that FAILED would be a lie in the one
// direction that costs money: FAILED tells the user to re-send, and re-sending a
// stock entry that has no id creates a *second* stock. So an unknown outcome is
// its own status, it does not raise exit 8, and its advice is to go and look
// rather than to re-run.
const (
	bulkCreated   = "CREATED"
	bulkUpdated   = "UPDATED"
	bulkUnchanged = "UNCHANGED"
	bulkFailed    = "FAILED"
	bulkUnknown   = "UNKNOWN"
)

// bulkResult is what became of one item in a bulk write.
//
// It is fft's own shape, not the API's, and that is deliberate: a chunked upsert
// makes several requests, so there is no single API response to print. Under
// -o json this is what a script gets, and it is the only rendering that can
// describe an item the API never answered for.
type bulkResult struct {
	// Key identifies the item as the user named it: a tenantArticleId for a
	// listing, an id or a tenantArticleId for a stock.
	Key string `json:"key"`

	// Facility is the facility the item was written to, where the answer named one.
	Facility string `json:"facility,omitempty"`

	// Status is one of the four above.
	Status string `json:"status"`

	// Reason is why a FAILED item failed. Empty for the others.
	Reason string `json:"reason,omitempty"`
}

// partialError reports a bulk write that some items survived and others did not.
//
// It is exit code 8, and it exists so that a script can tell "nothing happened"
// (a plain failure) from "some of it happened" — which are very different things
// to wake up to. The successful items are real and are not rolled back; the
// command has already printed the per-item table saying which were which.
type partialError struct {
	// Failed and Total are counted from the rendered results, so the number in the
	// message is the number in the table. Nothing is inferred.
	Failed int
	Total  int

	// Noun names the items, plural: "listings", "stocks".
	Noun string
}

func (e *partialError) Error() string {
	return fmt.Sprintf("%d of %d %s failed to be written; the rest were", e.Failed, e.Total, e.Noun)
}

// ExitCode is 8: a bulk operation with per-item failures. Not 1, because the
// operation partly succeeded, and a script that retries it wholesale would redo
// the part that worked.
func (e *partialError) ExitCode() int { return exitcode.Partial }

// Hint tells the user what to do about it.
func (e *partialError) Hint() string {
	return "The FAILED rows above say why. Fix them and re-send only those — the rest already landed."
}

// chunkRejected reports whether err is the API refusing *this chunk*, as opposed
// to refusing fft.
//
// The distinction decides whether a chunked write carries on. A 400 ("category
// ref 'shoes' does not exist") is about the entries in the request: the next chunk
// holds different entries and may well be fine, so it is sent, and this chunk's
// entries are reported FAILED. A 401, a 403, a 429 or a 500 is not about the
// entries at all — every remaining chunk would fail the same way, and pressing on
// would turn one expired token into four thousand rows reading "the token is not
// valid", which is a report that tells the user nothing and buries the one line
// that would have.
//
// So those abort, and the command exits on the *cause* — exit 4 for an expired
// token, not exit 8 for a partial write that never happened.
func chunkRejected(err error) bool {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	switch apiErr.Status {
	case http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

var bulkHeaders = []string{"KEY", "FACILITY", "STATUS", "REASON"}

// renderBulk prints the per-item outcome of a bulk write and reports a partial
// success as the exit-8 error it is.
//
// The table goes to stdout because it *is* the command's data — the point of a
// bulk write is which items landed. The counts go to stderr, because a count is
// metadata, and a script piping this into jq should not have to filter it out.
func renderBulk(deps *Deps, noun string, results []bulkResult) error {
	if len(results) == 0 {
		return deps.Printer.Empty(noun)
	}

	style := deps.Printer.Style()
	rows := make([][]string, 0, len(results))
	counts := map[string]int{}

	for _, r := range results {
		counts[r.Status]++
		rows = append(rows, []string{
			field(style, r.Key),
			field(style, r.Facility),
			bulkStatusCell(style, r.Status),
			field(style, r.Reason),
		})
	}

	if err := deps.Printer.Render(output.Rows{Headers: bulkHeaders, Rows: rows}, results); err != nil {
		return err
	}

	deps.Printer.Notef("%s.", bulkSummary(counts, len(results), noun))

	// An UNKNOWN is a write that landed and could not be described. It is not a
	// failure and must not be re-sent blindly, so it warns rather than raising
	// exit 8 — and the warning says to go and look.
	if unknown := counts[bulkUnknown]; unknown > 0 {
		deps.Printer.Warnf(
			"%d %s were written but the API's answer could not be read, so fft cannot say what happened to them. "+
				"Check them before re-sending: an entry with no id would be created a second time.",
			unknown, noun)
	}

	// The failures are counted from the rows that were actually rendered, so the
	// error's number and the table's rows can never disagree.
	if failed := counts[bulkFailed]; failed > 0 {
		return &partialError{Failed: failed, Total: len(results), Noun: noun}
	}
	return nil
}

// reportAborted prints what a bulk write managed to do before it was cut short.
//
// The run is about to fail on its cause — an expired token, a dropped connection,
// a ^C — and that is the right exit code. But the items already written are
// written, and a user who is not told which ones has no safe way to retry: for a
// stock create, re-running the file duplicates everything that landed. So the rows
// that are known go to stdout as usual, and the summary error is left to the
// caller.
//
// Any failure to render is deliberately swallowed: the command is already failing,
// and a rendering error would replace the cause the user actually needs to see.
func reportAborted(deps *Deps, noun string, results []bulkResult) {
	if len(results) == 0 {
		return
	}

	deps.Printer.Warnf("The run stopped early. These %s were already written:", noun)
	_ = renderBulk(deps, noun, results)
}

// bulkSummary phrases the counts, naming only the outcomes that occurred. A line
// reading "0 created, 0 updated, 12 unchanged, 0 failed" makes the reader do the
// filtering that fft should have done.
func bulkSummary(counts map[string]int, total int, noun string) string {
	var parts []string
	for _, status := range []string{bulkCreated, bulkUpdated, bulkUnchanged, bulkFailed, bulkUnknown} {
		if n := counts[status]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ToLower(status)))
		}
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%d %s written", total, noun)
	}
	return fmt.Sprintf("%d %s: %s", total, noun, strings.Join(parts, ", "))
}

// bulkStatusCell colours the column the eye goes to. A failure in a table of two
// hundred rows has to be findable without grep.
func bulkStatusCell(style output.Style, status string) string {
	switch status {
	case bulkCreated, bulkUpdated:
		return style.Green(status)
	case bulkUnchanged:
		return style.Faint(status)
	case bulkFailed:
		return style.Red(status)
	case bulkUnknown:
		return style.Yellow(status)
	default:
		return field(style, status)
	}
}
