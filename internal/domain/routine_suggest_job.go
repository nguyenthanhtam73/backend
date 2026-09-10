package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RoutineSuggestJobStatus is the pollable state of an AI routine suggestion.
type RoutineSuggestJobStatus string

const (
	SuggestJobProcessing RoutineSuggestJobStatus = "processing"
	SuggestJobCompleted  RoutineSuggestJobStatus = "completed"
	SuggestJobFailed     RoutineSuggestJobStatus = "failed"
	SuggestJobCancelled  RoutineSuggestJobStatus = "cancelled"
)

// RoutineSuggestJob persists AI suggest work so poll/cancel survive
// process restart (and a future multi-replica deploy).
type RoutineSuggestJob struct {
	ID        uuid.UUID               `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    uuid.UUID               `gorm:"type:uuid;not null;index" json:"user_id"`
	Status    RoutineSuggestJobStatus `gorm:"size:24;not null;index" json:"status"`
	Request   json.RawMessage         `gorm:"type:jsonb" json:"-"`
	Result    json.RawMessage         `gorm:"type:jsonb" json:"-"`
	ErrorMsg  string                  `gorm:"type:text" json:"-"`
	ExpiresAt time.Time               `gorm:"not null;index" json:"expires_at"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (RoutineSuggestJob) TableName() string { return "routine_suggest_jobs" }

func (j *RoutineSuggestJob) BeforeCreate(tx *gorm.DB) error {
	if j == nil {
		return nil
	}
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	return nil
}

func (j *RoutineSuggestJob) IsExpired(now time.Time) bool {
	if j == nil {
		return true
	}
	return !j.ExpiresAt.After(now.UTC())
}
