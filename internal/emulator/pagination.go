package emulator

import (
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

// The emulator serves the two paginations the client speaks, and borrows the
// client's own bounds so one --size rule holds on both sides of the wire.
const (
	defaultSearchSize = client.DefaultSize     // 20
	defaultListSize   = client.DefaultListSize // 25
	minSize           = client.MinSize         // 1
	maxSize           = client.MaxSize         // 250
)

// clampSize applies a page-size default and bounds. A zero (absent) size takes the
// default; anything outside the range is pulled back into it rather than rejected —
// the emulator is lenient where the real API is strict, so a fuzzed size never turns
// into a 400 the caller has to decode.
func clampSize(size, fallback int) int {
	if size == 0 {
		size = fallback
	}
	if size < minSize {
		return minSize
	}
	if size > maxSize {
		return maxSize
	}
	return size
}

// cursorPage is one page of a cursor search, ready to be wrapped in the pageInfo
// envelope the client decodes.
type cursorPage struct {
	items     []entityDoc
	hasNext   bool
	endCursor string
	total     *int // set only when the request asked for a total
}

// paginateCursor slices the collection by an opaque offset cursor.
//
// The cursor is the base64 of the next offset, so it strictly increases and is empty
// on the last page — which is exactly SearchAll's rule for a safe walk: it refuses to
// go on when told there is a next page but handed no new cursor, and this never tells
// it that. total is included only when withTotal was requested, because the client
// tells an absent total apart from a count of zero.
func paginateCursor(all []entityDoc, after string, size int, withTotal bool) (cursorPage, error) {
	offset, err := decodeCursor(after)
	if err != nil {
		return cursorPage{}, err
	}

	if offset > len(all) {
		offset = len(all)
	}
	end := min(offset+size, len(all))

	page := cursorPage{items: all[offset:end], hasNext: end < len(all)}
	if page.hasNext {
		page.endCursor = encodeCursor(end)
	}
	if withTotal {
		t := len(all)
		page.total = &t
	}
	return page, nil
}

// paginateAfterID slices the collection for a startAfterId GET list. The cursor is
// the last item's own id, so the next page begins just after the entity with that
// id; an empty id (the first page) or an id no longer present starts from the top.
func paginateAfterID(all []entityDoc, startAfterID string, size int) []entityDoc {
	start := 0
	if startAfterID != "" {
		for i, doc := range all {
			if idOf(doc) == startAfterID {
				start = i + 1
				break
			}
		}
	}
	return all[start:min(start+size, len(all))]
}

func encodeCursor(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeCursor(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return 0, fmt.Errorf("undecodable cursor %q", s)
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid cursor %q", s)
	}
	return n, nil
}
