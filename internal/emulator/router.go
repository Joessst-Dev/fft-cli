package emulator

import (
	"github.com/gofiber/fiber/v2"

	"github.com/Joessst-Dev/fft-cli/internal/api"
)

// registerRoutes wires every operation in the spec onto the Fiber app.
//
// Item routes (/api/{coll}/:id) are registered last, after every other route, so a
// literal sibling at the same depth — GET /api/{coll}/search, or any action endpoint
// the spec spells out — is matched before the parameter route swallows it. Fiber
// resolves routes in registration order, so order is the whole mechanism.
//
// A (method, path) already claimed is skipped rather than doubly registered: the
// versionless upstream spec can grow two operationIds on one method+path, and the
// first one registered is the one that answers.
func registerRoutes(app *fiber.App, ops []api.Operation, h *handlers) {
	// The emulator's own admin route, outside /api so it cannot shadow or be shadowed
	// by any spec operation. It triggers an event that no CRUD mutation would.
	app.Post("/_emulator/emit", h.emit)

	seen := map[string]bool{}
	add := func(method, path string, handler fiber.Handler) {
		key := method + " " + path
		if seen[key] {
			return
		}
		seen[key] = true
		app.Add(method, path, handler)
	}

	var items []api.Operation

	for _, op := range ops {
		coll, _, kind := classify(op)
		path := fiberPath(op.Path)

		switch kind {
		case kindList:
			add(op.Method, path, h.list(coll))
		case kindCreate:
			add(op.Method, path, h.create(coll))
		case kindSearch:
			add(op.Method, path, h.search(coll))
		case kindGet, kindUpdate, kindDelete:
			items = append(items, op) // deferred to the second pass
		default:
			add(op.Method, path, stateless([]byte(op.SampleResponse)))
		}
	}

	for _, op := range items {
		coll, idParam, kind := classify(op)
		path := fiberPath(op.Path)

		switch kind {
		case kindGet:
			add(op.Method, path, h.get(coll, idParam))
		case kindUpdate:
			add(op.Method, path, h.update(coll, idParam, op.Method == "PATCH"))
		case kindDelete:
			add(op.Method, path, h.remove(coll, idParam))
		}
	}
}
