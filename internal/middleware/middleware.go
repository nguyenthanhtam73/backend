package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// RegisterDefault wires safe defaults for a JSON API: panic recovery, request IDs, logging, CORS.
func RegisterDefault(app *fiber.App) {
	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${status} - ${latency} ${method} ${path} ${error}\n",
	}))
	app.Use(cors.New(cors.Config{
		// Use "*" so local dev works without per-origin config. Do not pair this with AllowCredentials: true
		// (browsers forbid * + credentials). Frontend must use Bearer in headers, not cookie sessions, unless you set explicit AllowOrigins.
		AllowOrigins:     "*",
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Preview-Token",
		AllowCredentials: false,
	}))
}
