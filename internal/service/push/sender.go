package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

var (
	// ErrNotConfigured means VAPID keys or repository are missing.
	ErrNotConfigured = errors.New("push sender not configured")
	// ErrNoSubscription means the user has no active push subscription.
	ErrNoSubscription = errors.New("no active push subscription")
	// ErrSendFailed means the push service rejected or failed the delivery.
	ErrSendFailed = errors.New("push send failed")
	// ErrSubscriptionExpired means the endpoint is gone (404/410); row was deleted.
	ErrSubscriptionExpired = errors.New("push subscription expired")
)

// Default asset paths served by the Next.js public/ folder.
const (
	DefaultNotificationIcon = "/icons/icon-192.png"
	// DefaultNotificationBadge is the small Android status-bar glyph.
	// Prefer a simple monochrome asset when one exists; colour 192 still works.
	DefaultNotificationBadge = "/icons/icon-192.png"
	// DefaultNotificationImage is the Android/Chrome "big picture" hero.
	DefaultNotificationImage = "/icons/icon-512.png"
	DefaultNotificationURL   = "/check-in"
)

// NotificationAction is a button shown on supported platforms (Chrome/Android).
type NotificationAction struct {
	Action string `json:"action"`
	Title  string `json:"title"`
}

// NotificationPayload is the JSON body delivered to the service worker.
// Keep in sync with frontend/public/sw.js push handler.
// Prefer BuildNotificationPayload(type, data) instead of filling this by hand.
type NotificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Icon  string `json:"icon,omitempty"`
	// Badge is the small status-bar icon (Android). Override via data["badge"].
	Badge string `json:"badge,omitempty"`
	// Image is the large hero / big-picture asset (Android Chrome).
	Image    string `json:"image,omitempty"`
	Tag      string `json:"tag,omitempty"`
	Renotify bool   `json:"renotify,omitempty"`
	// Timestamp is Unix milliseconds — shown as “sent at” on supporting UIs.
	Timestamp int64 `json:"timestamp,omitempty"`
	// Silent suppresses the platform sound/vibration when true.
	// Custom sound files are NOT supported by Web Push — the OS chooses audio.
	Silent bool `json:"silent"`
	// RequireInteraction keeps the notification until the user acts (important alerts).
	RequireInteraction bool `json:"requireInteraction,omitempty"`
	// Vibrate is a pattern in ms (Chrome/Android). Ignored when unsupported or Silent.
	Vibrate []int                `json:"vibrate,omitempty"`
	Data    map[string]any       `json:"data,omitempty"`
	Actions []NotificationAction `json:"actions,omitempty"`
}

// PushSender delivers Web Push messages and cleans up dead subscriptions.
type PushSender struct {
	cfg  *config.Config
	repo *repository.GormPushSubscriptionRepository
}

// NewPushSender constructs a sender. Returns a usable instance even when VAPID
// keys are empty — Send* then returns ErrNotConfigured (lets the API boot).
func NewPushSender(
	cfg *config.Config,
	repo *repository.GormPushSubscriptionRepository,
) *PushSender {
	return &PushSender{cfg: cfg, repo: repo}
}

// Configured reports whether VAPID keys are present.
func (s *PushSender) Configured() bool {
	return s != nil && s.cfg != nil && s.cfg.HasVAPIDKeys() && s.repo != nil
}

// SendToUser loads the user's active subscription and delivers a rich payload.
// Ready for future job workers: one call per user in a fan-out loop.
func (s *PushSender) SendToUser(
	ctx context.Context,
	userID uuid.UUID,
	payload NotificationPayload,
) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}

	sub, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if sub == nil {
		return ErrNoSubscription
	}
	return s.SendToSubscription(ctx, sub, payload)
}

// SendToSubscription encrypts and POSTs a notification to one endpoint.
// On 404/410 (expired / unsubscribed), the row is deleted automatically.
func (s *PushSender) SendToSubscription(
	ctx context.Context,
	subscription *domain.PushSubscription,
	payload NotificationPayload,
) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	if subscription == nil {
		return fmt.Errorf("subscription required")
	}

	normalized, err := normalizePayload(payload)
	if err != nil {
		return err
	}

	raw, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal push payload: %w", err)
	}

	wpSub := &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys: webpush.Keys{
			Auth:   subscription.Auth,
			P256dh: subscription.P256dh,
		},
	}

	ttl := TTLForPayload(normalized)
	opts := &webpush.Options{
		Subscriber:      s.cfg.VAPID.Subject,
		VAPIDPublicKey:  s.cfg.VAPID.PublicKey,
		VAPIDPrivateKey: s.cfg.VAPID.PrivateKey,
		TTL:             ttl,
	}

	resp, err := webpush.SendNotificationWithContext(ctx, raw, wpSub, opts)
	if err != nil {
		kind := classifyTransportError(err)
		slog.Error("push: send transport failed",
			"kind", kind,
			"user_id", subscription.UserID.String(),
			"subscription_id", subscription.ID.String(),
			"endpoint_suffix", endpointSuffix(subscription.Endpoint),
			"ttl", ttl,
			"err", err,
		)
		return fmt.Errorf("%w: %s: %v", ErrSendFailed, kind, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	kind := classifyHTTPStatus(resp.StatusCode)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		slog.Info("push: sent",
			"user_id", subscription.UserID.String(),
			"subscription_id", subscription.ID.String(),
			"tag", normalized.Tag,
			"status", resp.StatusCode,
			"ttl", ttl,
			"require_interaction", normalized.RequireInteraction,
			"silent", normalized.Silent,
		)
		return nil

	case resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound:
		slog.Warn("push: expired subscription, deleting",
			"kind", kind,
			"user_id", subscription.UserID.String(),
			"subscription_id", subscription.ID.String(),
			"status", resp.StatusCode,
			"endpoint_suffix", endpointSuffix(subscription.Endpoint),
		)
		if delErr := s.repo.DeleteByEndpoint(ctx, subscription.Endpoint); delErr != nil {
			slog.Error("push: failed to delete stale subscription",
				"endpoint_suffix", endpointSuffix(subscription.Endpoint),
				"err", delErr,
			)
		}
		return fmt.Errorf("%w: %w (HTTP %d)", ErrSendFailed, ErrSubscriptionExpired, resp.StatusCode)

	default:
		slog.Error("push: delivery rejected",
			"kind", kind,
			"user_id", subscription.UserID.String(),
			"subscription_id", subscription.ID.String(),
			"status", resp.StatusCode,
			"body", string(respBody),
			"endpoint_suffix", endpointSuffix(subscription.Endpoint),
		)
		return fmt.Errorf("%w: %s (HTTP %d)", ErrSendFailed, kind, resp.StatusCode)
	}
}

// classifyHTTPStatus maps push-service status codes to stable log labels.
func classifyHTTPStatus(status int) string {
	switch status {
	case http.StatusNotFound, http.StatusGone:
		return "expired_subscription"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "vapid_auth_failed"
	case http.StatusBadRequest:
		return "invalid_request"
	default:
		if status >= 500 {
			return "push_service_error"
		}
		if status >= 400 {
			return "client_error"
		}
		return "ok"
	}
}

// classifyTransportError labels dial/TLS/timeout failures before any HTTP status.
func classifyTransportError(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout_or_canceled"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "tls"), strings.Contains(msg, "x509"):
		return "tls_error"
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "no such host"):
		return "network_error"
	default:
		return "transport_error"
	}
}

// normalizePayload fills defaults so the service worker always gets a complete shape.
func normalizePayload(p NotificationPayload) (NotificationPayload, error) {
	p.Title = strings.TrimSpace(p.Title)
	p.Body = strings.TrimSpace(p.Body)
	if p.Title == "" {
		return NotificationPayload{}, fmt.Errorf("title is required")
	}
	if p.Body == "" {
		return NotificationPayload{}, fmt.Errorf("body is required")
	}
	if strings.TrimSpace(p.Icon) == "" {
		p.Icon = DefaultNotificationIcon
	}
	if strings.TrimSpace(p.Badge) == "" {
		p.Badge = DefaultNotificationBadge
	}
	if strings.TrimSpace(p.Image) == "" {
		p.Image = DefaultNotificationImage
	}
	if p.Timestamp <= 0 {
		p.Timestamp = time.Now().UnixMilli()
	}
	if p.Silent {
		// Silent notifications should not vibrate.
		p.Vibrate = nil
	} else if len(p.Vibrate) == 0 {
		p.Vibrate = append([]int(nil), DefaultVibrate...)
	} else {
		clean := make([]int, 0, len(p.Vibrate))
		for _, ms := range p.Vibrate {
			if ms > 0 {
				clean = append(clean, ms)
			}
		}
		if len(clean) == 0 {
			clean = append([]int(nil), DefaultVibrate...)
		}
		p.Vibrate = clean
	}
	if p.Data == nil {
		p.Data = map[string]any{}
	}
	if url, _ := p.Data["url"].(string); strings.TrimSpace(url) == "" {
		p.Data["url"] = DefaultNotificationURL
	}
	if action, _ := p.Data["action"].(string); strings.TrimSpace(action) == "" {
		p.Data["action"] = "open"
	}
	return p, nil
}

func endpointSuffix(endpoint string) string {
	if len(endpoint) <= 48 {
		return endpoint
	}
	return "…" + endpoint[len(endpoint)-48:]
}
