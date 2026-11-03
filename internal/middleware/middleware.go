package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// webCookieOrigins are the pages that may store the HttpOnly access cookie.
// Keep this list aligned with lobbyWebOrigins in the lobby socket.
var webCookieOrigins = map[string]struct{}{
	"https://dadiary.vn":     {},
	"https://www.dadiary.vn": {},
}

// RegisterDefault wires safe defaults for a JSON API: panic recovery, request IDs, logging, CORS.
func RegisterDefault(app *fiber.App) {
	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(logger.New(logger.Config{
		// Path and status only. Do not add request headers or the body:
		// those can carry access tokens, emails, and account ids.
		Format: "[${time}] ${status} - ${latency} ${method} ${path} ${error}\n",
	}))
	// The wildcard CORS middleware cannot also set Allow-Credentials
	// (browsers reject * with credentials). For the two DaDiary pages,
	// overwrite that pair after the handler so Set-Cookie is stored.
	app.Use(webCredentialCORS())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "*",
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Preview-Token",
		AllowCredentials: false,
	}))
}

func webCredentialCORS() fiber.Handler {
	return func(c *fiber.Ctx) error {
		origin := strings.TrimSpace(c.Get("Origin"))
		if _, ok := webCookieOrigins[origin]; !ok {
			return c.Next()
		}
		if c.Method() == fiber.MethodOptions {
			writeWebCredentialCORS(c, origin)
			return c.SendStatus(fiber.StatusNoContent)
		}
		err := c.Next()
		writeWebCredentialCORS(c, origin)
		return err
	}
}

func writeWebCredentialCORS(c *fiber.Ctx, origin string) {
	c.Set(fiber.HeaderAccessControlAllowOrigin, origin)
	c.Set(fiber.HeaderAccessControlAllowCredentials, "true")
	c.Set(fiber.HeaderAccessControlAllowHeaders, "Origin, Content-Type, Accept, Authorization, X-Preview-Token")
	c.Set(fiber.HeaderAccessControlAllowMethods, "GET,POST,PUT,PATCH,DELETE,OPTIONS")
	c.Set(fiber.HeaderVary, "Origin")
}
