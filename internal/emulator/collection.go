package emulator

import (
	"encoding/json"
	"maps"
	"slices"

	"github.com/Joessst-Dev/fft-cli/internal/api"
)

// defaultIDField is the property the whole API — and every part of fft that reads an
// entity's id — keys entities by. RawID hardcodes it too.
const defaultIDField = "id"

// collectionMeta is what the emulator needs to know about a collection to answer a
// list or a search: the name of the array field the client looks the entities up by.
//
// That field is not the collection's path segment. The connections collection lives
// at /api/facilities/{id}/connections but its list envelope keys the array as
// "interFacilityConnections", and the client looks it up by that exact name. So the
// key is inferred from the operation's synthesized response, where it comes straight
// from the schema, and only falls back to the segment when there was nothing to read.
type collectionMeta struct {
	name     string
	itemsKey string
	idField  string
}

// inferCollections builds the per-collection metadata from the operation table.
//
// For every top-level list and search operation it reads the array property out of
// the synthesized response body and records it as that collection's items-key. A
// list operation's answer wins over a search's when both are present — they agree in
// practice, but the list envelope is the one the GET paginator decodes.
func inferCollections(ops []api.Operation) map[string]collectionMeta {
	metas := map[string]collectionMeta{}

	set := func(coll, key string, preferred bool) {
		if coll == "" || key == "" {
			return
		}
		m, ok := metas[coll]
		if !ok {
			m = collectionMeta{name: coll, itemsKey: coll, idField: defaultIDField}
		}
		// The segment-name default is always overridable; a real key only yields to a
		// preferred one.
		if m.itemsKey == coll || preferred {
			m.itemsKey = key
		}
		metas[coll] = m
	}

	for _, op := range ops {
		coll, _, kind := classify(op)
		switch kind {
		case kindList:
			set(coll, arrayKey(op.SampleResponse), true)
		case kindSearch:
			set(coll, arrayKey(op.SampleResponse), false)
		}
	}
	return metas
}

// arrayKey returns the name of the top-level property whose value is a JSON array —
// the entities in a list or search envelope. pageInfo is skipped; it is an object,
// so it would not be picked anyway, but naming it says why. "" when the response has
// no array property (or is not an object).
func arrayKey(sampleResponse string) string {
	if sampleResponse == "" {
		return ""
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(sampleResponse), &fields); err != nil {
		return ""
	}

	// Sorted, so the choice is deterministic when a response somehow carries two
	// arrays; the real envelopes carry exactly one.
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		if key == "pageInfo" {
			continue
		}
		if isJSONArray(fields[key]) {
			return key
		}
	}
	return ""
}

func isJSONArray(raw json.RawMessage) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}
