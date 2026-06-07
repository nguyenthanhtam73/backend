package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UsageFeature identifies a metered free-plan action.
type UsageFeature string

const (
	UsageRoutineSuggest    UsageFeature = "routine_suggest"
	UsageRoutineManualEdit UsageFeature = "routine_manual_edit"
)

// UsageEvent records one billable/quota action for monthly free-tier limits.
type UsageEvent struct {
	ID        uuid.UUID    `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    uuid.UUID    `gorm:"type:uuid;not null;index:idx_usage_user_feature_month,priority:1" json:"user_id"`
	Feature   UsageFeature `gorm:"size:32;not null;index:idx_usage_user_feature_month,priority:2" json:"feature"`
	CreatedAt time.Time    `gorm:"index:idx_usage_user_feature_month,priority:3" json:"created_at"`
}

func (UsageEvent) TableName() string {
	return "usage_events"
}

func (e *UsageEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
