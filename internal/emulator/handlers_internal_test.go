package emulator

import (
	"net/http/httptest"
	"strings"

	"github.com/gofiber/fiber/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the create handler", func() {
	// This covers the 507 branch: a collection filled to maxEntitiesPerCollection
	// answers Insufficient Storage rather than growing without bound. The store is
	// filled directly, so it costs no HTTP round trips.
	It("answers 507 when the collection is at its cap", func() {
		store := NewStore(map[string]collectionMeta{})
		c := store.collection("orders")
		for i := range maxEntitiesPerCollection {
			id := synthID(i + 1)
			c.byID[id] = entityDoc{defaultIDField: id}
			c.order = append(c.order, id)
		}

		events := newEventEmitter(Config{}, store)
		DeferCleanup(func() { _ = events.Close() })

		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/api/orders", (&handlers{store: store, events: events}).create("orders"))

		req := httptest.NewRequest("POST", "/api/orders", strings.NewReader(`{"tenantOrderId":"x"}`))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(fiber.StatusInsufficientStorage))
	})
})
