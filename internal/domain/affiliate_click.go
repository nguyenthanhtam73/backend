package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AffiliateClickSource identifies which AI surface surfaced the product link.
type AffiliateClickSource string

const (
	AffiliateClickDailyFeedback    AffiliateClickSource = "daily_feedback"
	AffiliateClickRoutineSuggest   AffiliateClickSource = "routine_suggest"
	AffiliateClickStarterRoutine   AffiliateClickSource = "starter_routine"
)

// AllAffiliateClickSources is the allow-list for POST /affiliate/clicks.
var AllAffiliateClickSources = []AffiliateClickSource{
	AffiliateClickDailyFeedback,
	AffiliateClickRoutineSuggest,
	AffiliateClickStarterRoutine,
}

// IsValidAffiliateClickSource reports whether v is supported.
func IsValidAffiliateClickSource(v string) bool {
	for _, s := range AllAffiliateClickSources {
		if string(s) == v {
			return true
		}
	}
	return false
}

// AffiliateClick logs when a user taps an affiliate product link from the coach UI.
type AffiliateClick struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID        uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	ProductName   string    `gorm:"size:200;not null" json:"product_name"`
	Brand         string    `gorm:"size:120" json:"brand,omitempty"`
	AffiliateLink string    `gorm:"size:512;not null" json:"affiliate_link"`
	Source        string    `gorm:"size:32;not null;index" json:"source"`
	ContextID     string    `gorm:"size:64" json:"context_id,omitempty"`
	PriceRange    string    `gorm:"size:32" json:"price_range,omitempty"`
	Priority      string    `gorm:"size:16" json:"priority,omitempty"`

	CreatedAt time.Time `json:"created_at"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (AffiliateClick) TableName() string {
	return "affiliate_clicks"
}

func (a *AffiliateClick) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}
