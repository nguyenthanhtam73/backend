// Package response provides small, consistent JSON helpers for HTTP handlers.
package response

import "github.com/gofiber/fiber/v2"

// JSON writes a standardized success envelope.
func JSON(c *fiber.Ctx, status int, data any) error {
	return c.Status(status).JSON(fiber.Map{
		"success": true,
		"data":    data,
	})
}

// Error writes a standardized error envelope.
func Error(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"success": false,
		"error": fiber.Map{
			"code":    code,
			"message": message,
		},
	})
}
