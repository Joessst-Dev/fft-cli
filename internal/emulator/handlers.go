package emulator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// handlers is the set of request handlers, all reading and writing the one store.
type handlers struct {
	store *Store
}

// list answers GET /api/{coll} with the startAfterId envelope: the entities under
// their inferred items-key, and a total that is always present and accurate — the
// client's ListAll cross-checks a short page against it to know when to stop.
func (h *handlers) list(coll string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		all := h.store.List(coll)
		size := clampSize(atoiOr(c.Query("size"), 0), defaultListSize)
		page := paginateAfterID(all, c.Query("startAfterId"), size)

		return writeJSON(c, fiber.StatusOK, map[string]any{
			h.store.meta(coll).itemsKey: toAnySlice(page),
			"total":                     len(all),
		})
	}
}

// search answers POST /api/{coll}/search with the cursor envelope. It omits the
// total unless the request asked for it, because the client tells an absent total
// apart from a count of zero.
func (h *handlers) search(coll string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		req, err := decodeSearch(c.Body())
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, err.Error())
		}

		all := h.store.List(coll)
		size := clampSize(req.size, defaultSearchSize)
		page, err := paginateCursor(all, req.after, size, req.withTotal)
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, err.Error())
		}

		body := map[string]any{
			h.store.meta(coll).itemsKey: toAnySlice(page.items),
			"pageInfo": map[string]any{
				"hasNextPage":     page.hasNext,
				"endCursor":       page.endCursor,
				"hasPreviousPage": false,
				"startCursor":     "",
			},
		}
		if page.total != nil {
			body["total"] = *page.total
		}
		return writeJSON(c, fiber.StatusOK, body)
	}
}

// create answers POST /api/{coll}, storing the body and returning it with its new id
// and version.
func (h *handlers) create(coll string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		doc, err := decodeBody(c.Body())
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, err.Error())
		}
		return writeJSON(c, fiber.StatusCreated, h.store.Create(coll, doc))
	}
}

// get answers GET /api/{coll}/{id}, 404-ing an id the store does not hold so that a
// delete-then-get and a get-before-create both behave like the real API.
func (h *handlers) get(coll, idParam string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := c.Params(idParam)
		id, ok := h.resolve(coll, raw)
		if !ok {
			return writeError(c, fiber.StatusNotFound, fmt.Sprintf("no %s with id %q", coll, raw))
		}
		doc, _ := h.store.Get(coll, id)
		return writeJSON(c, fiber.StatusOK, doc)
	}
}

// update answers PUT/PATCH /api/{coll}/{id}, enforcing the body-carried optimistic
// lock and bumping the version.
func (h *handlers) update(coll, idParam string, patch bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := c.Params(idParam)
		doc, err := decodeBody(c.Body())
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, err.Error())
		}

		id, ok := h.resolve(coll, raw)
		if !ok {
			return writeError(c, fiber.StatusNotFound, fmt.Sprintf("no %s with id %q", coll, raw))
		}

		updated, err := h.store.Update(coll, id, doc, patch)
		if err != nil {
			var conflict *conflictError
			if errors.As(err, &conflict) {
				return writeConflict(c, conflict.requestVersion, conflict.version)
			}
			var notFound *notFoundError
			if errors.As(err, &notFound) {
				return writeError(c, fiber.StatusNotFound, err.Error())
			}
			return writeError(c, fiber.StatusInternalServerError, err.Error())
		}
		return writeJSON(c, fiber.StatusOK, updated)
	}
}

// remove answers DELETE /api/{coll}/{id}.
func (h *handlers) remove(coll, idParam string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := c.Params(idParam)
		id, ok := h.resolve(coll, raw)
		if !ok || !h.store.Delete(coll, id) {
			return writeError(c, fiber.StatusNotFound, fmt.Sprintf("no %s with id %q", coll, raw))
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// resolve turns a path id parameter into the id the store keeps. A value the store
// holds directly is used as is — a platform id. Otherwise a URN selector of the form
// urn:fft:<type>:<field>:<value> (the shape client.FacilityRef builds) is resolved by
// matching that field, exactly as the real API resolves it.
func (h *handlers) resolve(coll, param string) (string, bool) {
	if _, ok := h.store.Get(coll, param); ok {
		return param, true
	}
	if field, value, ok := parseURN(param); ok {
		return h.store.FindBy(coll, field, value)
	}
	return "", false
}

// parseURN pulls the selector field and value out of a urn:fft:<type>:<field>:<value>
// path id. ok is false for anything that is not such a URN.
func parseURN(s string) (field, value string, ok bool) {
	if !strings.HasPrefix(strings.ToLower(s), "urn:") {
		return "", "", false
	}
	parts := strings.Split(s, ":")
	if len(parts) < 5 {
		return "", "", false
	}
	return parts[3], strings.Join(parts[4:], ":"), true
}

// stateless answers with the operation's synthesized response — the long tail the
// emulator does not keep state for. An operation with no JSON response (a 204, or a
// non-JSON one) gets an empty 204.
func stateless(sample []byte) fiber.Handler {
	body := bytes.TrimSpace(sample)
	return func(c *fiber.Ctx) error {
		if len(body) == 0 {
			return c.SendStatus(fiber.StatusNoContent)
		}
		c.Status(fiber.StatusOK).Type("json")
		return c.Send(body)
	}
}

// searchRequest is the part of the client's search body the emulator acts on.
type searchRequest struct {
	after     string
	size      int
	withTotal bool
}

func decodeSearch(body []byte) (searchRequest, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return searchRequest{}, nil
	}

	var raw struct {
		After   *string `json:"after"`
		Size    *int    `json:"size"`
		Options *struct {
			WithTotal *bool `json:"withTotal"`
		} `json:"options"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return searchRequest{}, fmt.Errorf("invalid search body: %w", err)
	}

	req := searchRequest{}
	if raw.After != nil {
		req.after = *raw.After
	}
	if raw.Size != nil {
		req.size = *raw.Size
	}
	if raw.Options != nil && raw.Options.WithTotal != nil {
		req.withTotal = *raw.Options.WithTotal
	}
	return req, nil
}

// decodeBody reads a request body into a document, decoding numbers as json.Number
// so 64-bit ids and versions round-trip — the same choice decodeDoc makes elsewhere
// in fft. An empty body is an empty document, not an error.
func decodeBody(body []byte) (entityDoc, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return entityDoc{}, nil
	}

	var doc entityDoc
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	return doc, nil
}

// toAnySlice is a non-nil []any, so an empty page marshals to [] rather than null —
// the difference between "no results" and a decode error on the client.
func toAnySlice(docs []entityDoc) []any {
	out := make([]any, 0, len(docs))
	for _, d := range docs {
		out = append(out, d)
	}
	return out
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}
