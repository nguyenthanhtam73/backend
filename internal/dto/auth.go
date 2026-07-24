package dto

import (
	"time"

	"github.com/dadiary/backend/internal/domain"
)

// RegisterRequest is the JSON body for POST /api/v1/auth/register.
type RegisterRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	Username       string `json:"username,omitempty"` // optional; derived from email if empty
	DisplayName    string `json:"display_name,omitempty"`
	TurnstileToken string `json:"turnstile_token,omitempty"` // Cloudflare Turnstile widget token when captcha enabled
}

// LoginRequest is the JSON body for POST /api/v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RefreshRequest is the JSON body for POST /api/v1/auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LogoutRequest is the optional JSON body for POST /api/v1/auth/logout.
// When refresh_token is present it is revoked first; all user sessions are then revoked.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token,omitempty"`
}

// AuthTokensResponse is returned after successful register, login, or refresh.
type AuthTokensResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"` // seconds until access token expiry
}

// UserPublic is a safe projection of domain.User for API responses.
// Subscription fields power /pricing cancel + grace UI (i18n keys on the client).
type UserPublic struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Provider    string `json:"provider"`
	IsActive    bool   `json:"is_active"`
	// PlanTier is the *effective* tier (past grace → free).
	PlanTier string `json:"plan_tier,omitempty"`
	// PlanExpiresAt is RFC3339 when the billed/trial period ends (before grace).
	PlanExpiresAt string `json:"plan_expires_at,omitempty"`

	// --- Subscription lifecycle (GET /me) ---
	SubscriptionStatus string `json:"subscription_status,omitempty"`
	TrialEndsAt        string `json:"trial_ends_at,omitempty"`
	CanceledAt         string `json:"canceled_at,omitempty"`
	GraceEndsAt        string `json:"grace_ends_at,omitempty"`
	// DaysLeft until access ends (incl. grace). -1 = lifetime grant.
	DaysLeft          int  `json:"days_left"`
	InGrace           bool `json:"in_grace"`
	CancelAtPeriodEnd bool `json:"cancel_at_period_end"`
	EligibleForTrial  bool `json:"eligible_for_trial"`

	IsAdmin   bool   `json:"is_admin,omitempty"`
	CreatedAt string `json:"created_at"`
}

// SubscriptionSnapshot is a minimal view applied onto UserPublic (from SubscriptionService).
type SubscriptionSnapshot struct {
	Active            bool
	PlanTier          string
	Status            string
	PlanExpiresAt     *time.Time
	TrialEndsAt       *time.Time
	CanceledAt        *time.Time
	GraceEndsAt       *time.Time
	DaysLeft          int
	InGrace           bool
	CancelAtPeriodEnd bool
	EligibleForTrial  bool
}

// UserFromDomain maps a domain user to a public DTO (no secrets).
func UserFromDomain(u *domain.User) UserPublic {
	return UserFromDomainWithAdmin(u, false)
}

// UserFromDomainWithAdmin maps a domain user and sets the admin flag for /me.
// Uses DefaultGraceDays for effective tier / days_left; prefer ApplySubscriptionSnapshot
// when the configured grace window is available.
func UserFromDomainWithAdmin(u *domain.User, isAdmin bool) UserPublic {
	if u == nil {
		return UserPublic{}
	}
	now := time.Now().UTC()
	grace := domain.DefaultGraceDays
	out := UserPublic{
		ID:                 u.ID.String(),
		Email:              u.Email,
		Username:           u.Username,
		DisplayName:        u.DisplayName,
		AvatarURL:          u.AvatarURL,
		Provider:           string(u.Provider),
		IsActive:           u.IsActive,
		PlanTier:           string(domain.EffectivePlanTierWithGrace(u, now, grace)),
		SubscriptionStatus: string(domain.ResolveSubscriptionStatus(u, now, grace)),
		DaysLeft:           domain.DaysLeftUntilAccessEnd(u, now, grace),
		InGrace:            domain.InGracePeriod(u, now, grace),
		CancelAtPeriodEnd:  u.CanceledAt != nil && domain.EffectivePlanTierWithGrace(u, now, grace).IsPaidPlan(),
		EligibleForTrial:   domain.EligibleForTrial(u, now, grace),
		IsAdmin:            isAdmin,
		CreatedAt:          u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if u.PlanExpiresAt != nil {
		out.PlanExpiresAt = u.PlanExpiresAt.UTC().Format(time.RFC3339)
	}
	if u.TrialEndsAt != nil {
		out.TrialEndsAt = u.TrialEndsAt.UTC().Format(time.RFC3339)
	}
	if u.CanceledAt != nil {
		out.CanceledAt = u.CanceledAt.UTC().Format(time.RFC3339)
	}
	if ge := domain.GraceEndsAt(u.PlanExpiresAt, grace); ge != nil {
		out.GraceEndsAt = ge.UTC().Format(time.RFC3339)
	}
	return out
}

// ApplySubscriptionSnapshot overlays CheckActivePlan / Cancel results onto /me.
func ApplySubscriptionSnapshot(pub *UserPublic, snap SubscriptionSnapshot) {
	if pub == nil {
		return
	}
	if snap.PlanTier != "" {
		pub.PlanTier = snap.PlanTier
	}
	if snap.Status != "" {
		pub.SubscriptionStatus = snap.Status
	}
	pub.DaysLeft = snap.DaysLeft
	pub.InGrace = snap.InGrace
	pub.CancelAtPeriodEnd = snap.CancelAtPeriodEnd
	pub.EligibleForTrial = snap.EligibleForTrial
	if snap.PlanExpiresAt != nil {
		pub.PlanExpiresAt = snap.PlanExpiresAt.UTC().Format(time.RFC3339)
	} else if !snap.Active {
		pub.PlanExpiresAt = ""
	}
	if snap.TrialEndsAt != nil {
		pub.TrialEndsAt = snap.TrialEndsAt.UTC().Format(time.RFC3339)
	}
	if snap.CanceledAt != nil {
		pub.CanceledAt = snap.CanceledAt.UTC().Format(time.RFC3339)
	} else {
		pub.CanceledAt = ""
	}
	if snap.GraceEndsAt != nil {
		pub.GraceEndsAt = snap.GraceEndsAt.UTC().Format(time.RFC3339)
	} else {
		pub.GraceEndsAt = ""
	}
}
