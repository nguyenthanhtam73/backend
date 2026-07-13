package handler

import (
	"errors"
	"strings"

	"github.com/dadiary/backend/internal/dto"
	betasignupuc "github.com/dadiary/backend/internal/usecase/betasignup"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

// BetaSignupHandler serves the public Beta waitlist signup endpoint.
type BetaSignupHandler struct {
	svc *betasignupuc.Service
}

// NewBetaSignupHandler constructs handler.
func NewBetaSignupHandler(svc *betasignupuc.Service) *BetaSignupHandler {
	return &BetaSignupHandler{svc: svc}
}

// Create handles POST /api/v1/beta-signups.
func (h *BetaSignupHandler) Create(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "beta signup unavailable")
	}

	var body dto.CreateBetaSignupRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}

	res, err := h.svc.Create(c.UserContext(), body)
	if err != nil {
		return mapBetaSignupError(c, err)
	}
	return response.JSON(c, fiber.StatusCreated, res)
}

func mapBetaSignupError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, betasignupuc.ErrUnavailable):
		return response.Error(c, fiber.StatusServiceUnavailable, "beta_signup_unavailable", err.Error())
	case errors.Is(err, betasignupuc.ErrEmailAlreadyRegistered):
		return response.Error(c, fiber.StatusConflict, "email_already_registered", err.Error())
	default:
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "invalid beta signup request"
		}
		// Validation errors from ValidateAndMap arrive as fmt.Errorf strings.
		if strings.Contains(strings.ToLower(msg), "email") {
			return response.Error(c, fiber.StatusBadRequest, "invalid_email", msg)
		}
		return response.Error(c, fiber.StatusInternalServerError, "internal_error", "could not save beta signup")
	}
}
