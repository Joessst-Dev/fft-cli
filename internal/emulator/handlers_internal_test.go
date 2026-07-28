package emulator

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestCreateHandlerRefusesPastCap covers the 507 branch of the create handler: a
// collection filled to maxEntitiesPerCollection answers Insufficient Storage rather
// than growing without bound. The store is filled directly, so this costs no HTTP
// round trips.
func TestCreateHandlerRefusesPastCap(t *testing.T) {
	store := NewStore(map[string]collectionMeta{})
	c := store.collection("orders")
	for i := range maxEntitiesPerCollection {
		id := synthID(i + 1)
		c.byID[id] = entityDoc{defaultIDField: id}
		c.order = append(c.order, id)
	}

	events := newEventEmitter(Config{}, store)
	defer func() { _ = events.Close() }()

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Post("/api/orders", (&handlers{store: store, events: events}).create("orders"))

	req := httptest.NewRequest("POST", "/api/orders", strings.NewReader(`{"tenantOrderId":"x"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusInsufficientStorage {
		t.Fatalf("want 507 Insufficient Storage, got %d", resp.StatusCode)
	}
}
