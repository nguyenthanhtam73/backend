package domain

import (
	"math"
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
	// PlanExpiresAt is when the paid/trial period ends (before grace).
	// nil = no expiry (admin lifetime grant, or Free tier).
	PlanExpiresAt *time.Time `gorm:"index" json:"plan_expires_at,omitempty"`
	// TrialEndsAt marks the end of the one-time free trial (also proves trial was used).
	TrialEndsAt *time.Time `json:"trial_ends_at,omitempty"`
	// CanceledAt is set when the user cancels; access continues until grace ends.
	CanceledAt *time.Time `json:"canceled_at,omitempty"`
	// SubscriptionStatus is the billing lifecycle (none/trialing/active/canceled/past_due/expired).
	SubscriptionStatus SubscriptionStatus `gorm:"size:16;not null;default:none;index" json:"subscription_status"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// EffectivePlanTier returns the entitlements tier at `now` using DefaultGraceDays.
// Prefer EffectivePlanTierWithGrace when the caller has config.
func EffectivePlanTier(u *User, now time.Time) PlanTier {
	return EffectivePlanTierWithGrace(u, now, DefaultGraceDays)
}

// EffectivePlanTierWithGrace returns the entitlements tier at `now`.
//
// Paid access continues through the grace window after plan_expires_at.
// Lifetime grants (nil plan_expires_at) stay paid until explicitly revoked.
// Gates must not wait on the daily cron to clear plan_tier.
func EffectivePlanTierWithGrace(u *User, now time.Time, graceDays int) PlanTier {
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
	graceEnd := GraceEndsAt(u.PlanExpiresAt, graceDays)
	if graceEnd == nil || !graceEnd.After(now.UTC()) {
		return PlanFree
	}
	return tier
}

// ResolveSubscriptionStatus derives the live status from timestamps at `now`.
// Stored SubscriptionStatus may lag until cron / cancel / renew writes catch up;
// this is the source of truth for API responses and gates.
func ResolveSubscriptionStatus(u *User, now time.Time, graceDays int) SubscriptionStatus {
	if u == nil {
		return SubStatusNone
	}
	now = now.UTC()
	graceDays = ClampGraceDays(graceDays)
	tier := NormalizePlanTier(u.PlanTier)

	if !tier.IsPaidPlan() {
		stored := NormalizeSubscriptionStatus(u.SubscriptionStatus)
		if stored == SubStatusExpired {
			return SubStatusExpired
		}
		if u.TrialEndsAt != nil {
			return SubStatusExpired
		}
		return SubStatusNone
	}

	// Lifetime paid grant.
	if u.PlanExpiresAt == nil {
		if u.CanceledAt != nil {
			return SubStatusCanceled
		}
		return SubStatusActive
	}

	expires := u.PlanExpiresAt.UTC()
	graceEnd := expires.Add(DaysDuration(graceDays))

	if !graceEnd.After(now) {
		return SubStatusExpired
	}
	if u.CanceledAt != nil {
		return SubStatusCanceled
	}
	if !expires.After(now) {
		return SubStatusPastDue
	}
	// Still inside the billed/trial window.
	if NormalizeSubscriptionStatus(u.SubscriptionStatus) == SubStatusTrialing ||
		(u.TrialEndsAt != nil && !u.TrialEndsAt.Before(expires) && !u.TrialEndsAt.Before(now)) {
		return SubStatusTrialing
	}
	return SubStatusActive
}

// InGracePeriod is true when plan_expires_at has passed but grace has not.
func InGracePeriod(u *User, now time.Time, graceDays int) bool {
	if u == nil || u.PlanExpiresAt == nil {
		return false
	}
	if !NormalizePlanTier(u.PlanTier).IsPaidPlan() {
		return false
	}
	now = now.UTC()
	expires := u.PlanExpiresAt.UTC()
	if expires.After(now) {
		return false
	}
	graceEnd := GraceEndsAt(u.PlanExpiresAt, graceDays)
	return graceEnd != nil && graceEnd.After(now)
}

// AccessEndsAt is when Premium entitlements finally stop (expiry + grace, or nil = lifetime).
func AccessEndsAt(u *User, graceDays int) *time.Time {
	if u == nil || !NormalizePlanTier(u.PlanTier).IsPaidPlan() {
		return nil
	}
	if u.PlanExpiresAt == nil {
		return nil
	}
	return GraceEndsAt(u.PlanExpiresAt, graceDays)
}

// DaysLeftUntilAccessEnd returns whole days remaining (ceil) until AccessEndsAt.
// -1 means lifetime / unknown. 0 means ended or ending today.
func DaysLeftUntilAccessEnd(u *User, now time.Time, graceDays int) int {
	end := AccessEndsAt(u, graceDays)
	if end == nil {
		if u != nil && NormalizePlanTier(u.PlanTier).IsPaidPlan() && u.PlanExpiresAt == nil {
			return -1 // lifetime
		}
		return 0
	}
	remaining := end.UTC().Sub(now.UTC())
	if remaining <= 0 {
		return 0
	}
	return int(math.Ceil(remaining.Hours() / 24))
}

// EligibleForTrial is true when the user has never started a trial and is not paid.
func EligibleForTrial(u *User, now time.Time, graceDays int) bool {
	if u == nil {
		return false
	}
	if u.TrialEndsAt != nil {
		return false
	}
	return !EffectivePlanTierWithGrace(u, now, graceDays).IsPaidPlan()
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
	if u.SubscriptionStatus == "" {
		u.SubscriptionStatus = SubStatusNone
	}
	return nil
}
