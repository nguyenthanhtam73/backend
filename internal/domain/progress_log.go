package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ProgressLogEntryType categorizes timeline rows (before/after, milestones, free notes).
type ProgressLogEntryType string

const (
	ProgressLogEntryNote        ProgressLogEntryType = "note"
	ProgressLogEntryMilestone   ProgressLogEntryType = "milestone"
	ProgressLogEntryBeforeAfter ProgressLogEntryType = "before_after"
)

// ProgressLog is a timeline milestone for before/after or weekly reflections.
type ProgressLog struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	EntryType ProgressLogEntryType `gorm:"size:24;default:note;index" json:"entry_type"`
	LogDate   time.Time            `gorm:"type:date;not null;index" json:"log_date"`
	Title     string               `gorm:"size:200" json:"title,omitempty"`
	Body      string               `gorm:"type:text" json:"body,omitempty"`
	ImageURLs json.RawMessage      `gorm:"type:jsonb" json:"image_urls,omitempty"`
	Meta      json.RawMessage      `gorm:"type:jsonb" json:"meta,omitempty"` // e.g. pair_id, comparison labels

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (ProgressLog) TableName() string {
	return "progress_logs"
}

func (p *ProgressLog) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}
