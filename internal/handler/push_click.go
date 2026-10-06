package handler

import (
	"errors"
	"time"

	"github.com/dadiary/backend/internal/middleware"
	pushclickuc "github.com/dadiary/backend/internal/usecase/pushclick"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// PushClickHandler serves POST /api/v1/me/push/click.
type PushClickHandler struct {
	svc *pushclickuc.Service
}

// NewPushClickHandler constructs the handler.
func NewPushClickHandler(svc *pushclickuc.Service) *PushClickHandler {
	return &PushClickHandler{svc: svc}
}

type pushClickBody struct {
	Kind           string `json:"kind"`
	Tag            string `json:"tag"`
	IdempotencyKey string `json:"idempotency_key"`
	ClickedAt      string `json:"clicked_at"`
}

// Click records a notification click for the authenticated user.
// The service worker should call this from notificationclick (the page may
// relay the call with the user's access token).
func (h *PushClickHandler) Click(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "push click unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body pushClickBody
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
		}
	}
	in := pushclickuc.Input{
		Kind:           body.Kind,
		Tag:            body.Tag,
		IdempotencyKey: body.IdempotencyKey,
	}
	if body.ClickedAt != "" {
		t, err := time.Parse(time.RFC3339, body.ClickedAt)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_clicked_at", "clicked_at must be RFC3339")
		}
		in.ClickedAt = t
	}
	res, err := h.svc.Record(c.UserContext(), uid, in)
	if err != nil {
		if errors.Is(err, pushclickuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "push click unavailable")
		}
		return response.Error(c, fiber.StatusInternalServerError, "push_click_error", "could not record click")
	}
	return response.JSON(c, fiber.StatusOK, res)
}
