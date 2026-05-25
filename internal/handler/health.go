package handler

import (
	"github.com/dadiary/backend/internal/config"
	"github.com/gofiber/fiber/v2"
)

// HealthHandler exposes liveness/readiness style endpoints.
type HealthHandler struct {
	cfg *config.Config
}

// NewHealthHandler constructs a HealthHandler.
func NewHealthHandler(cfg *config.Config) *HealthHandler {
	return &HealthHandler{cfg: cfg}
}

// Register attaches routes to the Fiber router group.
func (h *HealthHandler) Register(r fiber.Router) {
	r.Get("/health", h.Health)
	r.Get("/version", h.Version)
}

// Health returns a minimal OK payload for load balancers and docker health checks.
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": "ok",
		"env":    h.cfg.Env,
	})
}

// Version is a placeholder until CI injects build metadata (ldflags).
func (h *HealthHandler) Version(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"service": "dadiary-api",
		"version": "0.1.0-dev",
	})
}
