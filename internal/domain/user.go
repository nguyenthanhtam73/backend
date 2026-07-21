package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuthProvider identifies how the user signed up (local, OAuth, etc.).
type AuthProvider string

const (
	AuthProviderLocal  AuthProvider = "local"
	AuthProviderGoogle AuthProvider = "google"
	AuthProviderApple  AuthProvider = "apple"
)

// PlanTier is the subscription bucket for feature gating (free / premium / premium_plus).
type PlanTier string

const (
	PlanFree        PlanTier = "free"
	PlanPremium     PlanTier = "premium"
	PlanPremiumPlus PlanTier = "premium_plus"
)

// NormalizePlanTier maps unknown/empty values to PlanFree and accepts the three known tiers.
func NormalizePlanTier(raw PlanTier) PlanTier {
	switch PlanTier(strings.ToLower(strings.TrimSpace(string(raw)))) {
	case PlanPremium:
		return PlanPremium
	case PlanPremiumPlus:
		return PlanPremiumPlus
	default:
		return PlanFree
	}
}

// IsPaidPlan reports whether the tier unlocks paid (non-free) entitlements.
func (t PlanTier) IsPaidPlan() bool {
	switch NormalizePlanTier(t) {
	case PlanPremium, PlanPremiumPlus:
		return true
	default:
		return false
	}
}

// User is an account that logs skin check-ins and stores skincare data.
type User struct {
	ID           uuid.UUID    `gorm:"type:uuid;primaryKey" json:"id"`
	Email        string       `gorm:"uniqueIndex;size:255;not null" json:"email"`
	Username     string       `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string       `gorm:"size:255" json:"-"`
	DisplayName  string       `gorm:"size:128" json:"display_name"`
	AvatarURL    string       `gorm:"size:512" json:"avatar_url,omitempty"`
	Provider     AuthProvider `gorm:"size:32;default:local" json:"provider"`
	IsActive     bool         `gorm:"default:true" json:"is_active"`
	PlanTier     PlanTier     `gorm:"size:16;default:free" json:"plan_tier"`
	// PlanExpiresAt is when a paid plan lapses back to Free.
	// nil = no expiry (admin lifetime grant, or Free tier).
	PlanExpiresAt *time.Time `gorm:"index" json:"plan_expires_at,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// EffectivePlanTier returns the entitlements tier at `now`.
// Paid rows past plan_expires_at are treated as Free until the daily cron
// clears plan_tier in the DB (gates must not wait on that cleanup).
func EffectivePlanTier(u *User, now time.Time) PlanTier {
	if u == nil {
		return PlanFree
	}
	tier := NormalizePlanTier(u.PlanTier)
	if !tier.IsPaidPlan() {
		return PlanFree
	}
	if u.PlanExpiresAt == nil {
		return tier // lifetime / admin grant
	}
	if !u.PlanExpiresAt.After(now) {
		return PlanFree
	}
	return tier
}

// PlanExpiryDuration returns how long a SePay interval unlocks paid access.
func PlanExpiryDuration(interval BillingInterval) time.Duration {
	switch BillingInterval(strings.ToLower(strings.TrimSpace(string(interval)))) {
	case BillingYearly:
		return 365 * 24 * time.Hour
	default:
		return 30 * 24 * time.Hour
	}
}

// ComputePlanExpiry sets expiry to now+interval, or extends from current
// expiry when the user still has active paid time left (renewal).
func ComputePlanExpiry(interval BillingInterval, now time.Time, current *time.Time) time.Time {
	start := now.UTC()
	if current != nil && current.After(start) {
		start = current.UTC()
	}
	return start.Add(PlanExpiryDuration(interval))
}

func (User) TableName() string {
	return "users"
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}
