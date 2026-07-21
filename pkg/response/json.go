// Package response provides small, consistent JSON helpers for HTTP handlers.
package response

import (
	"github.com/dadiary/backend/internal/domain"
	"github.com/gofiber/fiber/v2"
)

// JSON writes a standardized success envelope.
// Shape: { "success": true, "data": ... } — keeps FE ApiEnvelope compatible.
func JSON(c *fiber.Ctx, status int, data any) error {
	return c.Status(status).JSON(fiber.Map{
		"success": true,
		"data":    data,
	})
}

// JSONWithMessage writes success data plus a human message.
// Shape: { "success": true, "data": ..., "message": "..." }.
func JSONWithMessage(c *fiber.Ctx, status int, data any, message string) error {
	body := fiber.Map{
		"success": true,
		"data":    data,
	}
	if message != "" {
		body["message"] = message
	}
	return c.Status(status).JSON(body)
}

// FromAppError maps domain.AppError to the standard error envelope.
func FromAppError(c *fiber.Ctx, err error) error {
	if ae, ok := domain.AsAppError(err); ok {
		return Error(c, ae.HTTPStatus, ae.Code, ae.Message)
	}
	return Error(c, fiber.StatusInternalServerError, "internal_error", "something went wrong")
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

// ErrorWithReason writes an error envelope with machine-readable reason / feature
// (used by premium feature gates: feature_denied, quota_exceeded, …).
func ErrorWithReason(c *fiber.Ctx, status int, code, message, reason, feature string) error {
	errBody := fiber.Map{
		"code":    code,
		"message": message,
	}
	if reason != "" {
		errBody["reason"] = reason
	}
	if feature != "" {
		errBody["feature"] = feature
	}
	return c.Status(status).JSON(fiber.Map{
		"success": false,
		"error":   errBody,
	})
}
