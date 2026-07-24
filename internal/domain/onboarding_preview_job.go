package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OnboardingPreviewJobStatus tracks guest starter-routine background work.
type OnboardingPreviewJobStatus string

const (
	PreviewJobPending OnboardingPreviewJobStatus = "pending"
	PreviewJobReady   OnboardingPreviewJobStatus = "ready"
	PreviewJobFailed  OnboardingPreviewJobStatus = "failed"
)

// OnboardingPreviewJob persists guest preview jobs so multi-instance deploys
// can poll reliably. Poll requires matching AccessToken (not just knowing the id).
type OnboardingPreviewJob struct {
	ID          uuid.UUID                  `gorm:"type:uuid;primaryKey" json:"id"`
	AccessToken string                     `gorm:"size:64;not null" json:"-"` // secret for GET poll
	Status      OnboardingPreviewJobStatus `gorm:"size:16;not null;index" json:"status"`
	// StarterJSON holds the current starter routine payload (scaffold or AI result).
	StarterJSON json.RawMessage `gorm:"type:jsonb" json:"-"`
	ExpiresAt   time.Time       `gorm:"not null;index" json:"expires_at"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName for GORM.
func (OnboardingPreviewJob) TableName() string { return "onboarding_preview_jobs" }

// IsExpired reports whether the job TTL has passed.
func (j *OnboardingPreviewJob) IsExpired(now time.Time) bool {
	if j == nil {
		return true
	}
	return !j.ExpiresAt.After(now.UTC())
}
