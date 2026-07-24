package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RefreshSession is a server-tracked refresh token (jti + hash) so logout can revoke.
type RefreshSession struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"` // JWT jti
	UserID    uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	TokenHash string    `gorm:"size:64;not null" json:"-"` // sha256 hex of raw refresh JWT
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName for GORM.
func (RefreshSession) TableName() string { return "refresh_sessions" }

// IsActive reports whether the session can still mint access tokens.
func (s *RefreshSession) IsActive(now time.Time) bool {
	if s == nil {
		return false
	}
	if s.RevokedAt != nil {
		return false
	}
	return s.ExpiresAt.After(now.UTC())
}
