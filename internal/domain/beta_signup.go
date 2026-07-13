package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const DefaultBetaSignupSource = "landing_page"

// BetaSignup stores an email collected from the public Beta waitlist form.
type BetaSignup struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Email     string    `gorm:"size:320;not null;uniqueIndex" json:"email"`
	Source    string    `gorm:"size:64;not null;default:landing_page;index" json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

func (BetaSignup) TableName() string {
	return "beta_signups"
}

func (b *BetaSignup) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.Source == "" {
		b.Source = DefaultBetaSignupSource
	}
	return nil
}
