package handler

import (
	"errors"
	"strings"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// PushSubscriptionHandler serves Web Push subscribe / unsubscribe endpoints.
type PushSubscriptionHandler struct {
	svc *pushuc.Service
}

// NewPushSubscriptionHandler constructs handler.
func NewPushSubscriptionHandler(svc *pushuc.Service) *PushSubscriptionHandler {
	return &PushSubscriptionHandler{svc: svc}
}

// Subscribe handles POST /api/v1/me/push/subscribe.
func (h *PushSubscriptionHandler) Subscribe(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "push unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	var body dto.SubscribePushRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	// Prefer client-supplied UA; fall back to the request header.
	if strings.TrimSpace(body.UserAgent) == "" {
		body.UserAgent = c.Get("User-Agent")
	}

	res, err := h.svc.Subscribe(c.UserContext(), uid, body)
	if err != nil {
		if errors.Is(err, pushuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "push_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusBadRequest, "invalid_subscription", err.Error())
	}
	return response.JSON(c, fiber.StatusCreated, res)
}

// Unsubscribe handles DELETE /api/v1/me/push/unsubscribe.
func (h *PushSubscriptionHandler) Unsubscribe(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "push unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	if err := h.svc.Unsubscribe(c.UserContext(), uid); err != nil {
		if errors.Is(err, pushuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "push_unavailable", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "push_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, dto.PushUnsubscribeResponse{
		Message: "unsubscribed",
	})
}

// GetActive handles GET /api/v1/me/push/subscription.
// Lets the Settings UI sync enabled/disabled state from the backend.
func (h *PushSubscriptionHandler) GetActive(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "push unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	res, err := h.svc.GetActiveSubscription(c.UserContext(), uid)
	if err != nil {
		if errors.Is(err, pushuc.ErrUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "push_unavailable", err.Error())
		}
		if errors.Is(err, pushuc.ErrNotFound) {
			return response.Error(c, fiber.StatusNotFound, "not_found", "no active push subscription")
		}
		return response.Error(c, fiber.StatusInternalServerError, "push_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// SendTest handles POST /api/v1/me/push/test.
// Sends a fixed test notification to the authenticated user's active subscription.
func (h *PushSubscriptionHandler) SendTest(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "push unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	res, err := h.svc.SendTestNotification(c.UserContext(), uid)
	if err != nil {
		if errors.Is(err, pushuc.ErrSenderUnavailable) {
			return response.Error(c, fiber.StatusServiceUnavailable, "push_not_configured", "VAPID keys are not configured")
		}
		if errors.Is(err, pushuc.ErrNotFound) {
			return response.Error(c, fiber.StatusNotFound, "not_found", "no active push subscription — enable notifications first")
		}
		if errors.Is(err, pushsvc.ErrSendFailed) {
			return response.Error(c, fiber.StatusBadGateway, "push_send_failed", err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, "push_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}
