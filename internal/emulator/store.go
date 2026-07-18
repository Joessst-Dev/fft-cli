package emulator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
)

// entityDoc is one entity, carried as a decoded document rather than a generated
// model — the same choice the rest of fft makes, and for the same reason: the spec's
// allOf-with-siblings schemas do not survive a round trip through the typed structs,
// so writes that went through them would silently drop fields.
type entityDoc = map[string]any

// conflictError is a version mismatch on an update: the caller sent requestVersion,
// the store holds version. The handler renders it as the 409 the client's optimistic
// locking expects.
type conflictError struct {
	requestVersion int
	version        int
}

func (e *conflictError) Error() string {
	return fmt.Sprintf("version conflict: sent %d, current is %d", e.requestVersion, e.version)
}

// notFoundError is a get/update/delete of an id the collection does not hold.
type notFoundError struct{ collection, id string }

func (e *notFoundError) Error() string {
	return fmt.Sprintf("%s %q not found", e.collection, e.id)
}

// Store is the emulator's in-memory tenant: a set of collections, each a map of
// entities by id with a stable insertion order for pagination. One RWMutex guards
// the whole thing — a dev-time emulator has no need for finer locking, and one lock
// is one fewer thing to reason about.
type Store struct {
	mu    sync.RWMutex
	cols  map[string]*collectionData
	metas map[string]collectionMeta
}

type collectionData struct {
	byID   map[string]entityDoc
	order  []string
	nextID int
}

// NewStore returns an empty store that knows each collection's response metadata
// (its items-key), inferred from the spec.
func NewStore(metas map[string]collectionMeta) *Store {
	return &Store{cols: map[string]*collectionData{}, metas: metas}
}

// meta returns the collection's metadata, falling back to the segment name as the
// items-key when the spec gave nothing to infer from.
func (s *Store) meta(name string) collectionMeta {
	if m, ok := s.metas[name]; ok {
		return m
	}
	return collectionMeta{name: name, itemsKey: name, idField: defaultIDField}
}

// collection returns the collection, creating it lazily on first use.
func (s *Store) collection(name string) *collectionData {
	c, ok := s.cols[name]
	if !ok {
		c = &collectionData{byID: map[string]entityDoc{}}
		s.cols[name] = c
	}
	return c
}

// Create stores a new entity, assigning it an id (unless the body carries one) and
// version 1 (unless the body carries a version, which a seed fixture captured from a
// real tenant does), and returns the stored document. The exported methods speak the
// concrete map[string]any rather than the internal entityDoc alias, which is the same
// type.
func (s *Store) Create(name string, doc entityDoc) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	c := s.collection(name)

	id := idOf(doc)
	for id == "" || c.byID[id] != nil {
		c.nextID++
		id = synthID(c.nextID)
	}

	version, ok := versionOf(doc)
	if !ok {
		version = 1
	}

	stored := cloneDoc(doc)
	stored[defaultIDField] = id
	stored["version"] = version

	c.byID[id] = stored
	c.order = append(c.order, id)
	return cloneDoc(stored)
}

// FindBy returns the id of the first entity in the collection whose string field
// equals value, in insertion order. It backs URN path resolution: the API addresses
// a facility as urn:fft:facility:tenantFacilityId:<value>, and this is how the
// emulator turns that selector back into the entity the store keeps.
func (s *Store) FindBy(name, field, value string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	c, ok := s.cols[name]
	if !ok {
		return "", false
	}
	for _, id := range c.order {
		if v, ok := c.byID[id][field].(string); ok && v == value {
			return id, true
		}
	}
	return "", false
}

// Get returns the entity, or false when the collection does not hold it.
func (s *Store) Get(name, id string) (map[string]any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	c, ok := s.cols[name]
	if !ok {
		return nil, false
	}
	doc, ok := c.byID[id]
	if !ok {
		return nil, false
	}
	return cloneDoc(doc), true
}

// List returns every entity in the collection, in insertion order. It clones on
// every call — the paginators then slice the result — which is O(n) per page and so
// O(n²) across a full walk. That is deliberate simplicity: a dev emulator holds tens
// of entities, not the millions where this would matter.
func (s *Store) List(name string) []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	c, ok := s.cols[name]
	if !ok {
		return nil
	}
	out := make([]entityDoc, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, cloneDoc(c.byID[id]))
	}
	return out
}

// Update replaces (PUT) or merges (PATCH) an entity and bumps its version.
//
// It enforces the body-carried optimistic lock: when the incoming document names a
// version and it does not match the stored one, the update is refused with a
// *conflictError so the client re-reads and retries. A missing version is taken as
// "whatever is current" rather than a conflict, so a PATCH need not echo it.
func (s *Store) Update(name, id string, doc entityDoc, patch bool) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.cols[name]
	if !ok || c.byID[id] == nil {
		return nil, &notFoundError{collection: name, id: id}
	}

	existing := c.byID[id]
	current, _ := versionOf(existing)

	if want, ok := versionOf(doc); ok && want != current {
		return nil, &conflictError{requestVersion: want, version: current}
	}

	var result entityDoc
	if patch {
		result = cloneDoc(existing)
		for k, v := range doc {
			result[k] = v
		}
	} else {
		result = cloneDoc(doc)
	}
	result[defaultIDField] = id
	result["version"] = current + 1

	c.byID[id] = result
	return cloneDoc(result), nil
}

// Delete removes an entity, reporting whether it was there.
func (s *Store) Delete(name, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.cols[name]
	if !ok || c.byID[id] == nil {
		return false
	}

	delete(c.byID, id)
	for i, existing := range c.order {
		if existing == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	return true
}

// idOf reads an entity's id, "" when it has none or it is not a string.
func idOf(d entityDoc) string {
	s, _ := d[defaultIDField].(string)
	return s
}

// synthID mints an id shaped like the platform's UUIDs, so it passes through
// client.FacilityRef unwrapped and `fft <noun> get <id>` addresses it directly. The
// counter is folded into the hex so a generated id is still legible in a log.
func synthID(n int) string {
	return fmt.Sprintf("%08x-0000-4000-8000-%012x", n, n)
}

// versionOf reads an entity's version across the several numeric shapes a decoded
// JSON document can hold it in (json.Number under UseNumber, float64 otherwise).
func versionOf(d entityDoc) (int, bool) {
	switch v := d["version"].(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	}
	return 0, false
}

// cloneDoc returns a deep copy so a caller cannot mutate what the store holds. The
// JSON round trip decodes with UseNumber, matching decodeDoc elsewhere in fft, so a
// 64-bit id or version is not rounded through a float.
func cloneDoc(d entityDoc) entityDoc {
	b, err := json.Marshal(d)
	if err != nil {
		return entityDoc{}
	}
	var out entityDoc
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return entityDoc{}
	}
	return out
}
