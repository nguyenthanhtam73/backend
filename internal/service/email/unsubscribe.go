package email

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

const unsubPurpose = "email_unsub_v1"

// UnsubscribeSigner creates and verifies HMAC unsubscribe tokens.
type UnsubscribeSigner struct {
	secret  []byte
	apiBase string
}

// NewUnsubscribeSigner uses the JWT secret (always present) so we do not need
// a second production key. apiBase is the public API origin (may be empty).
func NewUnsubscribeSigner(secret, apiBase string) *UnsubscribeSigner {
	return &UnsubscribeSigner{
		secret:  []byte(strings.TrimSpace(secret)),
		apiBase: strings.TrimRight(strings.TrimSpace(apiBase), "/"),
	}
}

// Ready reports whether tokens can be signed.
func (s *UnsubscribeSigner) Ready() bool {
	return s != nil && len(s.secret) > 0
}

// Token returns a URL-safe token for userID.
func (s *UnsubscribeSigner) Token(userID uuid.UUID) (string, error) {
	if !s.Ready() {
		return "", fmt.Errorf("unsubscribe signer not configured")
	}
	if userID == uuid.Nil {
		return "", fmt.Errorf("user id required")
	}
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(unsubPurpose + ":" + userID.String()))
	sig := hex.EncodeToString(mac.Sum(nil))
	raw := userID.String() + "." + sig
	return base64.RawURLEncoding.EncodeToString([]byte(raw)), nil
}

// Parse validates token and returns the user id.
func (s *UnsubscribeSigner) Parse(token string) (uuid.UUID, error) {
	var zero uuid.UUID
	if !s.Ready() {
		return zero, fmt.Errorf("unsubscribe signer not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return zero, fmt.Errorf("token required")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return zero, fmt.Errorf("invalid token")
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 2 {
		return zero, fmt.Errorf("invalid token")
	}
	uid, err := uuid.Parse(parts[0])
	if err != nil || uid == uuid.Nil {
		return zero, fmt.Errorf("invalid token")
	}
	want, err := s.Token(uid)
	if err != nil {
		return zero, err
	}
	if !hmac.Equal([]byte(want), []byte(token)) {
		return zero, fmt.Errorf("invalid token")
	}
	return uid, nil
}

// URL is the public unsubscribe link, or empty when the API origin is unknown.
func (s *UnsubscribeSigner) URL(userID uuid.UUID) string {
	if s == nil || s.apiBase == "" {
		return ""
	}
	tok, err := s.Token(userID)
	if err != nil {
		return ""
	}
	return s.apiBase + "/api/v1/email/unsubscribe?token=" + url.QueryEscape(tok)
}
