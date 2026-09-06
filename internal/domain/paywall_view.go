package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Paywall view surfaces the client may report. Keep in sync with
// frontend lib/analytics/funnel.ts PaywallSurface (+ "upgrade").
const (
	PaywallSurfaceUpsellBanner = "upsell_banner"
	PaywallSurfacePricing      = "pricing"
	PaywallSurfaceUpgrade      = "upgrade"
)

// AllPaywallSurfaces is the allow-list for POST /analytics/paywall-view.
var AllPaywallSurfaces = []string{
	PaywallSurfaceUpsellBanner,
	PaywallSurfacePricing,
	PaywallSurfaceUpgrade,
}

// IsValidPaywallSurface reports whether v is a supported impression surface.
func IsValidPaywallSurface(v string) bool {
	for _, s := range AllPaywallSurfaces {
		if s == v {
			return true
		}
	}
	return false
}

// PaywallView is one client-reported paywall / upsell / upgrade impression.
type PaywallView struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	UserID          *uuid.UUID `gorm:"type:uuid;index" json:"user_id,omitempty"`
	Surface         string     `gorm:"size:32;not null;index" json:"surface"`
	Feature         string     `gorm:"size:64;not null;default:generic" json:"feature"`
	RecommendedPlan string     `gorm:"size:32" json:"recommended_plan,omitempty"`
	CreatedAt       time.Time  `gorm:"index" json:"created_at"`
}

func (PaywallView) TableName() string {
	return "paywall_views"
}

func (p *PaywallView) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}
