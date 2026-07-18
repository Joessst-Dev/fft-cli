package emulator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// seed preloads entities from a directory of JSON fixtures. Each <collection>.json
// file seeds the collection named by its base name and holds either one entity or an
// array of them — so a demo or a test can start the emulator with data already in it
// rather than POSTing it first.
func seed(store *Store, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		coll := strings.TrimSuffix(entry.Name(), ".json")
		path := filepath.Join(dir, entry.Name())

		raw, err := os.ReadFile(path) // #nosec G304 -- a seed directory the user chose
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}

		docs, err := decodeSeed(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, doc := range docs {
			store.Create(coll, doc)
		}
	}
	return nil
}

// decodeSeed reads a fixture that is either a single object or an array of objects,
// decoding numbers as json.Number so seeded ids and versions round-trip intact.
func decodeSeed(raw []byte) ([]entityDoc, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()

	if trimmed[0] == '[' {
		var docs []entityDoc
		if err := dec.Decode(&docs); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return docs, nil
	}

	var doc entityDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return []entityDoc{doc}, nil
}
