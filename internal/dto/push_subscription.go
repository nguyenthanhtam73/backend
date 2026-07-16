package dto

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

const (
	maxPushEndpointRunes  = 2048
	maxPushKeyRunes       = 512
	maxPushUserAgentRunes = 512
)

// SubscribePushRequest is POST /api/v1/me/push/subscribe.
// Matches the shape returned by PushSubscription.toJSON() in browsers.
type SubscribePushRequest struct {
	Endpoint  string                   `json:"endpoint"`
	Keys      SubscribePushKeysRequest `json:"keys"`
	UserAgent string                   `json:"user_agent,omitempty"`
}

// SubscribePushKeysRequest holds the Web Push encryption keys.
type SubscribePushKeysRequest struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// PushSubscriptionResponse is returned after subscribe / get.
type PushSubscriptionResponse struct {
	ID        string `json:"id"`
	Endpoint  string `json:"endpoint"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// PushUnsubscribeResponse is DELETE /api/v1/me/push/unsubscribe.
type PushUnsubscribeResponse struct {
	Message string `json:"message"`
}

// PushTestResponse is POST /api/v1/me/push/test.
type PushTestResponse struct {
	Message string `json:"message"`
}

// ValidateAndMap converts the subscribe DTO into a domain row.
func (r SubscribePushRequest) ValidateAndMap(userID uuid.UUID) (*domain.PushSubscription, string) {
	if userID == uuid.Nil {
		return nil, "user id required"
	}
	endpoint := strings.TrimSpace(r.Endpoint)
	if endpoint == "" {
		return nil, "endpoint is required"
	}
	if utf8.RuneCountInString(endpoint) > maxPushEndpointRunes {
		return nil, "endpoint is too long"
	}
	if !strings.HasPrefix(endpoint, "https://") {
		return nil, "endpoint must be an https URL"
	}

	p256dh := strings.TrimSpace(r.Keys.P256dh)
	auth := strings.TrimSpace(r.Keys.Auth)
	if p256dh == "" {
		return nil, "keys.p256dh is required"
	}
	if auth == "" {
		return nil, "keys.auth is required"
	}
	if utf8.RuneCountInString(p256dh) > maxPushKeyRunes {
		return nil, "keys.p256dh is too long"
	}
	if utf8.RuneCountInString(auth) > maxPushKeyRunes {
		return nil, "keys.auth is too long"
	}

	ua := strings.TrimSpace(r.UserAgent)
	if utf8.RuneCountInString(ua) > maxPushUserAgentRunes {
		runes := []rune(ua)
		ua = string(runes[:maxPushUserAgentRunes])
	}

	return &domain.PushSubscription{
		UserID:    userID,
		Endpoint:  endpoint,
		P256dh:    p256dh,
		Auth:      auth,
		UserAgent: ua,
		IsActive:  true,
	}, ""
}

// FromDomainPushSubscription maps a persisted row to the API response
// (never exposes p256dh / auth to the client).
func FromDomainPushSubscription(row domain.PushSubscription) PushSubscriptionResponse {
	return PushSubscriptionResponse{
		ID:        row.ID.String(),
		Endpoint:  row.Endpoint,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
