package handler

import (
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// E2EHandler exposes gated helpers for Playwright SePay smoke tests.
// Routes are only registered when DADIARY_E2E_SECRET is set — never wire a
// weak secret into production.
type E2EHandler struct {
	cfg   *config.Config
	db    *gorm.DB
	users *repository.GormUserRepository
}

// NewE2EHandler constructs the handler (nil-safe; Register no-ops when disabled).
func NewE2EHandler(cfg *config.Config, db *gorm.DB, users *repository.GormUserRepository) *E2EHandler {
	return &E2EHandler{cfg: cfg, db: db, users: users}
}

// Register mounts POST /internal/e2e/force-plan when the E2E secret is configured.
func (h *E2EHandler) Register(api fiber.Router) {
	if h == nil || h.cfg == nil || !h.cfg.E2EHelpersEnabled() || h.db == nil || h.users == nil {
		return
	}
	api.Post("/internal/e2e/force-plan", h.ForcePlan)
}

type forcePlanBody struct {
	Email         string  `json:"email"`
	PlanTier      string  `json:"plan_tier"`
	PlanExpiresAt *string `json:"plan_expires_at"` // RFC3339; omit/null = lifetime (NULL)
}

// ForcePlan sets plan_tier + plan_expires_at for a user (smoke: expiry downgrade).
// Auth: header X-E2E-Secret must match DADIARY_E2E_SECRET.
func (h *E2EHandler) ForcePlan(c *fiber.Ctx) error {
	if h == nil || h.cfg == nil || !h.cfg.E2EHelpersEnabled() {
		return c.SendStatus(fiber.StatusNotFound)
	}
	got := strings.TrimSpace(c.Get("X-E2E-Secret"))
	if got == "" || got != h.cfg.E2ESecret {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "unauthorized", "message": "invalid e2e secret"},
		})
	}

	var body forcePlanBody
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "invalid_request", "message": "invalid json"},
		})
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	if email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "invalid_request", "message": "email required"},
		})
	}
	tier := domain.NormalizePlanTier(domain.PlanTier(body.PlanTier))

	var expiresAt *time.Time
	if body.PlanExpiresAt != nil && strings.TrimSpace(*body.PlanExpiresAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.PlanExpiresAt))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false,
				"error":   fiber.Map{"code": "invalid_request", "message": "plan_expires_at must be RFC3339"},
			})
		}
		utc := t.UTC()
		expiresAt = &utc
	}

	user, err := h.users.GetByEmail(c.UserContext(), email)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "internal", "message": err.Error()},
		})
	}
	if user == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "not_found", "message": "user not found"},
		})
	}

	err = h.db.WithContext(c.UserContext()).Transaction(func(tx *gorm.DB) error {
		_, uErr := h.users.UpdatePlanTierTx(tx, user.ID, tier, expiresAt)
		return uErr
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "internal", "message": err.Error()},
		})
	}

	updated, err := h.users.GetByID(c.UserContext(), user.ID)
	if err != nil || updated == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "internal", "message": "reload failed"},
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    dto.UserFromDomain(updated),
	})
}
