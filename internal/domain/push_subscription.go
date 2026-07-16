package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PushSubscription stores a browser Web Push subscription for one user.
//
// Phase 1 only persists the subscription so we can send later (Phase 2+).
// Keys (P256dh / Auth) come from PushSubscription.getKey() on the client.
type PushSubscription struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	// Endpoint is the push service URL (unique globally — one browser endpoint).
	Endpoint string `gorm:"type:text;not null;uniqueIndex" json:"endpoint"`
	// P256dh is the client public key (base64url) used to encrypt payloads.
	P256dh string `gorm:"type:text;not null" json:"p256dh"`
	// Auth is the auth secret (base64url) used with P256dh for encryption.
	Auth string `gorm:"type:text;not null" json:"auth"`
	// UserAgent is optional diagnostics (browser / OS) for support.
	UserAgent string `gorm:"type:text" json:"user_agent,omitempty"`
	// IsActive marks whether the subscription should receive pushes.
	// Phase 1 hard-deletes on unsubscribe; the flag prepares soft-disable later.
	IsActive  bool      `gorm:"not null;default:true;index" json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (PushSubscription) TableName() string {
	return "push_subscriptions"
}

func (p *PushSubscription) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}
