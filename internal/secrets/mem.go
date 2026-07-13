package secrets

import (
	"maps"
	"sync"
)

var _ Store = (*MemStore)(nil)

// MemStore keeps secrets in memory for the lifetime of the process. It backs the
// specs — which is why it is exported: they need [MemStore.Snapshot] to assert
// on exactly which keys were written.
type MemStore struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewMem returns an in-memory Store. It is safe for concurrent use.
func NewMem() *MemStore {
	return &MemStore{values: make(map[string]string)}
}

// Kind implements [Store].
func (*MemStore) Kind() string { return "memory" }

// Get implements [Store].
func (s *MemStore) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	val, ok := s.values[key]
	if !ok {
		return "", ErrNotFound
	}
	return val, nil
}

// Set implements [Store].
func (s *MemStore) Set(key, val string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.values[key] = val
	return nil
}

// Delete implements [Store]. Deleting an absent key is not an error.
func (s *MemStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.values, key)
	return nil
}

// Snapshot returns a copy of everything the store holds.
func (s *MemStore) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return maps.Clone(s.values)
}
