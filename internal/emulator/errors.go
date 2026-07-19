package emulator

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"

	"github.com/Joessst-Dev/fft-cli/internal/api"
)

// The API's error envelope is a JSON *array* of {summary, ...}, not an object —
// decoding it into a struct silently yields {}, which is how the real API turns a
// 404 into a blank line. The emulator has to speak the same shape or the client
// prints nothing.

// writeError answers with a single-summary error envelope at the given status.
func writeError(c *fiber.Ctx, status int, summary string) error {
	return writeErrors(c, status, []api.ErrorInner{{Summary: summary}})
}

// writeConflict answers with the 409 the client's optimistic locking reads: an
// envelope carrying both the version the request sent and the one the store holds,
// so the CLI can tell the user exactly how stale they were.
func writeConflict(c *fiber.Ctx, requestVersion, version int) error {
	rv, v := int64(requestVersion), int64(version)
	return writeErrors(c, fiber.StatusConflict, []api.ErrorInner{{
		Summary:        "version conflict",
		RequestVersion: &rv,
		Version:        &v,
	}})
}

func writeErrors(c *fiber.Ctx, status int, errs []api.ErrorInner) error {
	c.Status(status).Type("json")
	body, err := json.Marshal(errs)
	if err != nil {
		return err
	}
	return c.Send(body)
}

// writeJSON marshals v — a map holding decoded entities — and sends it as JSON. The
// entities carry json.Number values from the store, which encoding/json renders as
// the bare number, so ids and versions survive without being floated.
func writeJSON(c *fiber.Ctx, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, err.Error())
	}
	c.Status(status).Type("json")
	return c.Send(body)
}
