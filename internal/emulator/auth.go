package emulator

import "github.com/gofiber/fiber/v2"

// permissiveAuth accepts every request, bearer token or not. The emulator is a local
// dev server that guards nothing, so there is nothing to authenticate — pointing fft
// at it needs only the FFT_ID_TOKEN headless recipe, which never reaches here.
//
// It is a named middleware rather than nothing at all so a future token-enforcing
// mode has one obvious place to live.
func permissiveAuth() fiber.Handler {
	return func(c *fiber.Ctx) error { return c.Next() }
}
