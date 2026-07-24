package handler

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/security/turnstile"
	authuc "github.com/dadiary/backend/internal/usecase/auth"
	subscriptionuc "github.com/dadiary/backend/internal/usecase/subscription"
	"github.com/dadiary/backend/pkg/response"
)

// AuthHandler is the HTTP adapter for AuthUsecase (register / login / logout / refresh / me).
type AuthHandler struct {
	auth            *authuc.Usecase
	turnstileSecret string // non-empty: require verified Turnstile token on register
	cfg             *config.Config
	// Optional: enriches GET /me with configured grace / trial windows.
	subs *subscriptionuc.Service
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(auth *authuc.Usecase, cfg *config.Config) *AuthHandler {
	h := &AuthHandler{auth: auth, cfg: cfg}
	if cfg != nil {
		h.turnstileSecret = strings.TrimSpace(cfg.Turnstile.SecretKey)
	}
	return h
}

// AttachSubscription enables CheckActivePlan overlay on GET /me.
func (h *AuthHandler) AttachSubscription(subs *subscriptionuc.Service) {
	if h == nil {
		return
	}
	h.subs = subs
}

// RegisterRoutes attaches public /auth/* and JWT-gated GET /me (+ logout).
func (h *AuthHandler) RegisterRoutes(public fiber.Router, jwt fiber.Handler) {
	if jwt != nil {
		public.Get("/me", jwt, h.Me)
		public.Post("/auth/logout", jwt, h.Logout)
	}
	g := public.Group("/auth")
	g.Post("/register", h.Register)
	g.Post("/login", h.Login)
	g.Post("/refresh", h.Refresh)
}

// Register handles POST /auth/register.
func (h *AuthHandler) Register(c *fiber.Ctx) error {
	if h == nil || h.auth == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "authentication is not available")
	}
	var body dto.RegisterRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "request body must be valid JSON")
	}
	if h.turnstileSecret != "" {
		if err := turnstile.Verify(c.UserContext(), h.turnstileSecret, body.TurnstileToken, c.IP()); err != nil {
			switch {
			case errors.Is(err, turnstile.ErrMissingToken):
				return response.Error(c, fiber.StatusBadRequest, "captcha_required", "captcha verification is required")
			case errors.Is(err, turnstile.ErrVerificationFailed):
				return response.Error(c, fiber.StatusBadRequest, "captcha_failed", "captcha verification failed")
			default:
				return response.Error(c, fiber.StatusBadGateway, "captcha_unavailable", "could not verify captcha; try again")
			}
		}
	}
	res, err := h.auth.Register(c.UserContext(), body)
	if err != nil {
		return mapAuthError(c, err)
	}
	return response.JSONWithMessage(c, fiber.StatusCreated, fiber.Map{
		"tokens": res.Tokens,
		"user":   res.User,
	}, "registered")
}

// Login handles POST /auth/login.
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	if h == nil || h.auth == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "authentication is not available")
	}
	var body dto.LoginRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "request body must be valid JSON")
	}
	res, err := h.auth.Login(c.UserContext(), body)
	if err != nil {
		return mapAuthError(c, err)
	}
	return response.JSONWithMessage(c, fiber.StatusOK, fiber.Map{
		"tokens": res.Tokens,
		"user":   res.User,
	}, "logged_in")
}

// Refresh handles POST /auth/refresh (public — uses refresh_token body, not access JWT).
func (h *AuthHandler) Refresh(c *fiber.Ctx) error {
	if h == nil || h.auth == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "authentication is not available")
	}
	var body dto.RefreshRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "request body must be valid JSON")
	}
	res, err := h.auth.Refresh(c.UserContext(), body.RefreshToken)
	if err != nil {
		return mapAuthError(c, err)
	}
	return response.JSONWithMessage(c, fiber.StatusOK, fiber.Map{
		"tokens": res.Tokens,
		"user":   res.User,
	}, "refreshed")
}

// Logout handles POST /auth/logout (JWT required). Revokes refresh sessions server-side.
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	if h == nil || h.auth == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "authentication is not available")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "user context missing")
	}
	var body dto.LogoutRequest
	_ = c.BodyParser(&body) // body optional
	if err := h.auth.Logout(c.UserContext(), uid, body.RefreshToken); err != nil {
		return mapAuthError(c, err)
	}
	return response.JSONWithMessage(c, fiber.StatusOK, fiber.Map{
		"logged_out": true,
	}, "logged_out")
}

// Me handles GET /me (protected).
// Includes subscription lifecycle fields (status, days_left, in_grace, trial_ends_at, …).
func (h *AuthHandler) Me(c *fiber.Ctx) error {
	if h == nil || h.auth == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "authentication is not available")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "user context missing")
	}
	pub, err := h.auth.GetMe(c.UserContext(), uid)
	if err != nil {
		return mapAuthError(c, err)
	}
	pub.IsAdmin = h.cfg != nil && h.cfg.IsAdminEmail(pub.Email)

	// Overlay configured grace/trial days from SubscriptionService when wired.
	if h.subs != nil {
		if plan, planErr := h.subs.CheckActivePlan(c.UserContext(), uid); planErr == nil && plan != nil {
			dto.ApplySubscriptionSnapshot(&pub, dto.SubscriptionSnapshot{
				Active:            plan.Active,
				PlanTier:          string(plan.PlanTier),
				Status:            string(plan.Status),
				PlanExpiresAt:     plan.PlanExpiresAt,
				TrialEndsAt:       plan.TrialEndsAt,
				CanceledAt:        plan.CanceledAt,
				GraceEndsAt:       plan.GraceEndsAt,
				DaysLeft:          plan.DaysLeft,
				InGrace:           plan.InGrace,
				CancelAtPeriodEnd: plan.CancelAtPeriodEnd,
				EligibleForTrial:  plan.EligibleForTrial,
			})
		}
	}
	return response.JSONWithMessage(c, fiber.StatusOK, pub, "ok")
}

func mapAuthError(c *fiber.Ctx, err error) error {
	if ae, ok := domain.AsAppError(err); ok {
		return response.Error(c, ae.HTTPStatus, ae.Code, ae.Message)
	}
	// Legacy sentinel fallback (tests / unexpected wraps).
	switch {
	case errors.Is(err, authuc.ErrEmailTaken):
		return response.Error(c, fiber.StatusConflict, "email_taken", err.Error())
	case errors.Is(err, authuc.ErrUsernameTaken):
		return response.Error(c, fiber.StatusConflict, "username_taken", err.Error())
	case errors.Is(err, authuc.ErrInvalidCredentials):
		return response.Error(c, fiber.StatusUnauthorized, "invalid_credentials", err.Error())
	case errors.Is(err, authuc.ErrInvalidRefresh):
		return response.Error(c, fiber.StatusUnauthorized, "invalid_refresh", err.Error())
	case errors.Is(err, authuc.ErrUserInactive):
		return response.Error(c, fiber.StatusForbidden, "user_inactive", err.Error())
	case errors.Is(err, authuc.ErrUserNotFound):
		return response.Error(c, fiber.StatusNotFound, "user_not_found", err.Error())
	case errors.Is(err, authuc.ErrTokenConfig):
		return response.Error(c, fiber.StatusInternalServerError, "misconfigured", "authentication misconfigured")
	case errors.Is(err, authuc.ErrDatabase):
		return response.Error(c, fiber.StatusServiceUnavailable, "database_unavailable", "database is not available")
	case errors.Is(err, authuc.ErrInvalidInput):
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
	default:
		return response.Error(c, fiber.StatusInternalServerError, "internal_error", "something went wrong")
	}
}
