package email

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const svixTolerance = 5 * time.Minute

var (
	// ErrWebhookNotConfigured means the Resend/Svix signing secret is unset.
	ErrWebhookNotConfigured = errors.New("resend webhook secret not configured")
	// ErrInvalidWebhookSignature means the Svix signature, id, or timestamp failed.
	ErrInvalidWebhookSignature = errors.New("invalid resend webhook signature")
)

// VerifySvix checks a Resend webhook the way Svix documents it.
// secret is whsec_<base64> (the whsec_ prefix is optional).
// id, timestamp, and signature are the svix-id, svix-timestamp, and
// svix-signature header values (webhook-* aliases are accepted by the caller).
func VerifySvix(secret string, body []byte, id, timestamp, signature string, now time.Time) error {
	key, err := decodeSvixSecret(secret)
	if err != nil {
		if strings.TrimSpace(secret) == "" {
			return ErrWebhookNotConfigured
		}
		return ErrInvalidWebhookSignature
	}
	id = strings.TrimSpace(id)
	timestamp = strings.TrimSpace(timestamp)
	signature = strings.TrimSpace(signature)
	if id == "" || timestamp == "" || signature == "" || len(body) == 0 {
		return ErrInvalidWebhookSignature
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrInvalidWebhookSignature
	}
	if now.IsZero() {
		now = time.Now()
	}
	delta := now.Sub(time.Unix(ts, 0))
	if delta < -svixTolerance || delta > svixTolerance {
		return ErrInvalidWebhookSignature
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	expected := mac.Sum(nil)

	matched := false
	for _, part := range strings.Fields(signature) {
		ver, sig, ok := strings.Cut(part, ",")
		if !ok || ver != "v1" || sig == "" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(sig)
		if err != nil {
			continue
		}
		if hmac.Equal(got, expected) {
			matched = true
		}
	}
	if !matched {
		return ErrInvalidWebhookSignature
	}
	return nil
}

func decodeSvixSecret(secret string) ([]byte, error) {
	secret = strings.TrimSpace(secret)
	secret = strings.TrimPrefix(secret, "whsec_")
	if secret == "" {
		return nil, fmt.Errorf("empty secret")
	}
	return base64.StdEncoding.DecodeString(secret)
}
