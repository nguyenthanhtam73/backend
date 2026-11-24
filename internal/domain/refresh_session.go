package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// RefreshClientWeb is the default session kind (browser and anything that is not the Android app).
	RefreshClientWeb = "web"
	// RefreshClientAndroid is the only client value that receives the long app refresh TTL.
	RefreshClientAndroid = "android"
)

// NormalizeRefreshClient maps a client marker to a stored session kind.
// The native app sends X-DaDiary-Client: android-app on every API call,
// including skin checks. Older builds sent the exact value "android".
// Both select the app session (long refresh TTL and the polite coach voice).
// Anything else, including a different capitalisation, is a web session.
func NormalizeRefreshClient(raw string) string {
	switch strings.TrimSpace(raw) {
	case RefreshClientAndroid, "android-app":
		return RefreshClientAndroid
	default:
		return RefreshClientWeb
	}
}

// RefreshSession is a server-tracked refresh token (jti + hash) so logout can revoke.
type RefreshSession struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"` // JWT jti
	UserID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"user_id"`
	TokenHash string     `gorm:"size:64;not null" json:"-"` // sha256 hex of raw refresh JWT
	ExpiresAt time.Time  `gorm:"not null;index" json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	// ClientKind is web or android. Existing rows default to web via AutoMigrate.
	ClientKind string `gorm:"column:client_kind;size:16;not null;default:'web'" json:"-"`

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
