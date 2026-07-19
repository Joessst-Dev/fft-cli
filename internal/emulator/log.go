package emulator

import (
	"fmt"
	"io"

	"github.com/gofiber/fiber/v2"
)

// requestLogger writes one line per request — method, URL, status, request-body size
// — to w. It is the emulator's answer to the test harness's request recorder: with
// --verbose you see exactly what fft put on the wire.
func requestLogger(w io.Writer) fiber.Handler {
	if w == nil {
		w = io.Discard
	}
	return func(c *fiber.Ctx) error {
		size := len(c.Body())
		err := c.Next()
		fmt.Fprintf(w, "%s %s -> %d (%d bytes in)\n",
			c.Method(), c.OriginalURL(), c.Response().StatusCode(), size)
		return err
	}
}
