package handler

import (
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/pkg/alert"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// E2EHandler exposes gated helpers for Playwright SePay smoke tests.
// Routes are only registered when DADIARY_E2E_SECRET is set — never wire a
// weak secret into production.
type E2EHandler struct {
	cfg     *config.Config
	db      *gorm.DB
	users   *repository.GormUserRepository
	alerts  *alert.Recorder // optional — payment / webhook alert capture
	ops     *repository.PaymentOpsEventRepository
}

// NewE2EHandler constructs the handler (nil-safe; Register no-ops when disabled).
func NewE2EHandler(cfg *config.Config, db *gorm.DB, users *repository.GormUserRepository) *E2EHandler {
	return &E2EHandler{cfg: cfg, db: db, users: users}
}

// AttachAlertRecorder wires the in-memory alert tee used by GET /internal/e2e/alerts.
func (h *E2EHandler) AttachAlertRecorder(rec *alert.Recorder) *E2EHandler {
	if h != nil {
		h.alerts = rec
	}
	return h
}

// AttachOpsRepo enables GET /internal/e2e/ops-events (payment_ops_events rows).
func (h *E2EHandler) AttachOpsRepo(ops *repository.PaymentOpsEventRepository) *E2EHandler {
	if h != nil {
		h.ops = ops
	}
	return h
}

// Register mounts E2E helpers when the E2E secret is configured.
func (h *E2EHandler) Register(api fiber.Router) {
	if h == nil || h.cfg == nil || !h.cfg.E2EHelpersEnabled() || h.db == nil || h.users == nil {
		return
	}
	api.Post("/internal/e2e/force-plan", h.ForcePlan)
	api.Get("/internal/e2e/alerts", h.ListAlerts)
	api.Delete("/internal/e2e/alerts", h.ClearAlerts)
	api.Get("/internal/e2e/ops-events", h.ListOpsEvents)
}

func (h *E2EHandler) authorize(c *fiber.Ctx) bool {
	if h == nil || h.cfg == nil || !h.cfg.E2EHelpersEnabled() {
		return false
	}
	got := strings.TrimSpace(c.Get("X-E2E-Secret"))
	return got != "" && got == h.cfg.E2ESecret
}

type forcePlanBody struct {
	Email         string  `json:"email"`
	PlanTier      string  `json:"plan_tier"`
	PlanExpiresAt *string `json:"plan_expires_at"` // RFC3339; omit/null = lifetime (NULL)
}

// ForcePlan sets plan_tier + plan_expires_at for a user (smoke: expiry downgrade).
// Auth: header X-E2E-Secret must match DADIARY_E2E_SECRET.
func (h *E2EHandler) ForcePlan(c *fiber.Ctx) error {
	if !h.authorize(c) {
		if h == nil || h.cfg == nil || !h.cfg.E2EHelpersEnabled() {
			return c.SendStatus(fiber.StatusNotFound)
		}
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

// ListAlerts returns in-memory ops alerts captured since process start / last clear.
// Query: key (e.g. payment_success), invoice (optional).
func (h *E2EHandler) ListAlerts(c *fiber.Ctx) error {
	if !h.authorize(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if h.alerts == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "alerts_unavailable",
				"message": "alert recorder not wired (restart API with E2E secret)",
			},
		})
	}
	key := strings.TrimSpace(c.Query("key"))
	invoice := strings.TrimSpace(c.Query("invoice"))
	events := h.alerts.Find(key, invoice)
	rows := make([]fiber.Map, 0, len(events))
	for _, e := range events {
		rows = append(rows, fiber.Map{
			"key":           e.Key,
			"unique_suffix": e.UniqueSuffix,
			"title":         e.Title,
			"level":         string(e.Level),
			"message":       e.Message,
			"detail":        e.Detail,
			"fields":        e.Fields,
		})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"count":  len(rows),
			"alerts": rows,
		},
	})
}

// ClearAlerts resets the in-memory alert buffer.
func (h *E2EHandler) ClearAlerts(c *fiber.Ctx) error {
	if !h.authorize(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if h.alerts != nil {
		h.alerts.Clear()
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"cleared": true}})
}

// ListOpsEvents returns recent payment_ops_events (DB) for invoice/kind filters.
func (h *E2EHandler) ListOpsEvents(c *fiber.Ctx) error {
	if !h.authorize(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if h.ops == nil || h.db == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "ops_unavailable", "message": "ops repo not wired"},
		})
	}
	invoice := strings.TrimSpace(c.Query("invoice"))
	kind := strings.TrimSpace(c.Query("kind"))
	q := h.db.WithContext(c.UserContext()).Model(&domain.PaymentOpsEvent{}).Order("created_at DESC").Limit(50)
	if invoice != "" {
		q = q.Where("invoice_number = ?", invoice)
	}
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var rows []domain.PaymentOpsEvent
	if err := q.Find(&rows).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   fiber.Map{"code": "internal", "message": err.Error()},
		})
	}
	out := make([]fiber.Map, 0, len(rows))
	for _, r := range rows {
		out = append(out, fiber.Map{
			"id":             r.ID.String(),
			"kind":           r.Kind,
			"reason":         r.Reason,
			"invoice_number": r.InvoiceNumber,
			"created_at":     r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data":    fiber.Map{"count": len(out), "events": out},
	})
}
