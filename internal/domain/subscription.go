package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Subscription lifecycle defaults (clamped via config to these ranges).
const (
	DefaultTrialDays = 7
	MinTrialDays     = 7
	MaxTrialDays     = 14

	DefaultGraceDays = 3
	MinGraceDays     = 3
	MaxGraceDays     = 7
)

// SubscriptionStatus is the billing lifecycle state stored on users (+ history rows).
//
//	none      — never subscribed / free
//	trialing  — free trial window
//	active    — paid period in good standing
//	canceled  — user canceled; access until plan_expires_at + grace
//	past_due  — past plan_expires_at but still inside grace
//	expired   — grace ended; entitlements are Free
type SubscriptionStatus string

const (
	SubStatusNone     SubscriptionStatus = "none"
	SubStatusTrialing SubscriptionStatus = "trialing"
	SubStatusActive   SubscriptionStatus = "active"
	SubStatusCanceled SubscriptionStatus = "canceled"
	SubStatusPastDue  SubscriptionStatus = "past_due"
	SubStatusExpired  SubscriptionStatus = "expired"
)

// NormalizeSubscriptionStatus maps unknown/empty values to SubStatusNone.
func NormalizeSubscriptionStatus(raw SubscriptionStatus) SubscriptionStatus {
	switch SubscriptionStatus(strings.ToLower(strings.TrimSpace(string(raw)))) {
	case SubStatusTrialing:
		return SubStatusTrialing
	case SubStatusActive:
		return SubStatusActive
	case SubStatusCanceled:
		return SubStatusCanceled
	case SubStatusPastDue:
		return SubStatusPastDue
	case SubStatusExpired:
		return SubStatusExpired
	default:
		return SubStatusNone
	}
}

// ClampTrialDays keeps trial length inside the product range [7, 14].
func ClampTrialDays(days int) int {
	if days < MinTrialDays {
		return DefaultTrialDays
	}
	if days > MaxTrialDays {
		return MaxTrialDays
	}
	return days
}

// ClampGraceDays keeps grace length inside the product range [3, 7].
func ClampGraceDays(days int) int {
	if days < MinGraceDays {
		return DefaultGraceDays
	}
	if days > MaxGraceDays {
		return MaxGraceDays
	}
	return days
}

// DaysDuration converts whole calendar days to a duration (UTC wall-clock).
func DaysDuration(days int) time.Duration {
	if days < 0 {
		days = 0
	}
	return time.Duration(days) * 24 * time.Hour
}

// GraceEndsAt returns when paid access finally ends (expiry + grace).
// nil plan_expires_at → nil (lifetime / Free).
func GraceEndsAt(planExpiresAt *time.Time, graceDays int) *time.Time {
	if planExpiresAt == nil {
		return nil
	}
	end := planExpiresAt.UTC().Add(DaysDuration(ClampGraceDays(graceDays)))
	return &end
}

// SubscriptionEventType labels rows in the subscriptions history table.
type SubscriptionEventType string

const (
	SubEventTrialStarted SubscriptionEventType = "trial_started"
	SubEventRenewed      SubscriptionEventType = "renewed"
	SubEventCanceled     SubscriptionEventType = "canceled"
	SubEventExpired      SubscriptionEventType = "expired"
	SubEventGranted      SubscriptionEventType = "granted" // admin / internal
)

// SubscriptionProvider identifies who drove the lifecycle change.
type SubscriptionProvider string

const (
	SubProviderTrial SubscriptionProvider = "trial"
	SubProviderSePay SubscriptionProvider = "sepay"
	SubProviderAdmin SubscriptionProvider = "admin"
	SubProviderCron  SubscriptionProvider = "cron"
)

// Subscription is an append-only history row for trial / renew / cancel / expire.
// Current entitlement state lives on User (plan_tier, plan_expires_at, …).
type Subscription struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	PlanTier        PlanTier             `gorm:"size:16;not null" json:"plan_tier"`
	BillingInterval BillingInterval      `gorm:"size:16" json:"billing_interval,omitempty"`
	Status          SubscriptionStatus   `gorm:"size:16;not null;index" json:"status"`
	EventType       SubscriptionEventType `gorm:"size:32;not null;index" json:"event_type"`
	Provider        SubscriptionProvider `gorm:"size:16;not null;default:sepay" json:"provider"`

	// ExternalRef links to SePay invoice / admin reason code (optional).
	ExternalRef string `gorm:"size:128" json:"external_ref,omitempty"`

	TrialEndsAt        *time.Time `json:"trial_ends_at,omitempty"`
	PeriodStartsAt     *time.Time `json:"period_starts_at,omitempty"`
	PeriodEndsAt       *time.Time `json:"period_ends_at,omitempty"`
	CanceledAt         *time.Time `json:"canceled_at,omitempty"`
	GraceEndsAt        *time.Time `json:"grace_ends_at,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (Subscription) TableName() string {
	return "subscriptions"
}

func (s *Subscription) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	if s.Provider == "" {
		s.Provider = SubProviderSePay
	}
	s.Status = NormalizeSubscriptionStatus(s.Status)
	s.PlanTier = NormalizePlanTier(s.PlanTier)
	return nil
}
