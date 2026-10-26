// Package resendhook records Resend email.opened and email.clicked webhooks.
package resendhook

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/email"
	"github.com/google/uuid"
)

const maxWebhookBody = 64 * 1024

var (
	// ErrNotConfigured means DADIARY_RESEND_WEBHOOK_SECRET is unset.
	ErrNotConfigured = email.ErrWebhookNotConfigured
	// ErrInvalidSignature means Svix verification failed.
	ErrInvalidSignature = email.ErrInvalidWebhookSignature
	// ErrInvalidPayload means the signed body was not a Resend event.
	ErrInvalidPayload = errors.New("invalid resend webhook payload")
)

// Outcome is how one delivery was handled. Duplicate deliveries are OK.
type Outcome struct {
	Stored    bool
	Duplicate bool
	Ignored   bool
	EventType string
}

// Service verifies Svix signatures and stores open/click events.
type Service struct {
	secret   string
	events   *repository.EmailEngagementRepository
	receipts *repository.EmailSendReceiptRepository
	users    *repository.GormUserRepository
	now      func() time.Time
}

// SetClock overrides the signature timestamp check. Tests pin this to the signed instant.
func (s *Service) SetClock(now func() time.Time) {
	if s != nil && now != nil {
		s.now = now
	}
}

// NewService wires the webhook. secret may be empty (Handle then returns ErrNotConfigured).
func NewService(
	secret string,
	events *repository.EmailEngagementRepository,
	receipts *repository.EmailSendReceiptRepository,
	users *repository.GormUserRepository,
) *Service {
	return &Service{
		secret:   strings.TrimSpace(secret),
		events:   events,
		receipts: receipts,
		users:    users,
		now:      time.Now,
	}
}

// Handle verifies the webhook and stores opened/clicked events.
// Other event types return a successful no-op. A repeated Svix id is a no-op success.
func (s *Service) Handle(ctx context.Context, id, timestamp, signature string, body []byte) (Outcome, error) {
	var zero Outcome
	if s == nil || s.events == nil || s.receipts == nil {
		return zero, errors.New("resend webhook unavailable")
	}
	if s.secret == "" {
		return zero, ErrNotConfigured
	}
	if len(body) == 0 || len(body) > maxWebhookBody {
		return zero, ErrInvalidPayload
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	if err := email.VerifySvix(s.secret, body, id, timestamp, signature, now); err != nil {
		return zero, err
	}

	var payload resendEvent
	if err := json.Unmarshal(body, &payload); err != nil {
		return zero, ErrInvalidPayload
	}
	eventType, ok := normalizeEvent(payload.Type)
	if !ok {
		return Outcome{Ignored: true, EventType: strings.TrimSpace(payload.Type)}, nil
	}

	emailID := strings.TrimSpace(payload.Data.EmailID)
	var userID *uuid.UUID
	kind := ""
	if emailID != "" {
		rec, err := s.receipts.FindByResendEmailID(ctx, emailID)
		if err != nil {
			return zero, err
		}
		if rec != nil {
			if s.users == nil {
				return zero, errors.New("resend webhook user lookup unavailable")
			}
			account, err := s.users.GetByID(ctx, rec.UserID)
			if err != nil {
				return zero, err
			}
			// The receipt can outlive the account (user_id is replaced on
			// deletion). Do not insert a row and do not recreate the user.
			if account == nil {
				slog.Info("resend_webhook: ignored, account gone",
					"resend_event_id", strings.TrimSpace(id),
					"event_type", eventType,
				)
				return Outcome{Ignored: true, EventType: eventType}, nil
			}
			idCopy := rec.UserID
			userID = &idCopy
			kind = rec.Kind
		}
	}

	occurred := payload.occurredAt(now)
	link := ""
	if eventType == domain.EmailEventClicked {
		link = trimLink(payload.Data.Click.Link)
	}
	ev := &domain.EmailEngagementEvent{
		ResendEventID: strings.TrimSpace(id),
		ResendEmailID: emailID,
		UserID:        userID,
		Kind:          kind,
		EventType:     eventType,
		LinkURL:       link,
		OccurredAt:    occurred,
	}
	inserted, err := s.events.InsertIfNew(ctx, ev)
	if err != nil {
		return zero, err
	}
	if !inserted {
		slog.Info("resend_webhook: duplicate", "resend_event_id", ev.ResendEventID, "event_type", eventType)
		return Outcome{Duplicate: true, EventType: eventType}, nil
	}
	slog.Info("resend_webhook: stored",
		"resend_event_id", ev.ResendEventID,
		"resend_email_id", emailID,
		"event_type", eventType,
		"kind", kind,
		"user_known", userID != nil,
	)
	return Outcome{Stored: true, EventType: eventType}, nil
}

func normalizeEvent(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "email.opened":
		return domain.EmailEventOpened, true
	case "email.clicked":
		return domain.EmailEventClicked, true
	default:
		return "", false
	}
}

func trimLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if utf8.RuneCountInString(raw) <= 2048 && len(raw) <= 2048 {
		return raw
	}
	// Cut by bytes so the column stays inside VARCHAR(2048).
	if len(raw) > 2048 {
		raw = raw[:2048]
	}
	for !utf8.ValidString(raw) && len(raw) > 0 {
		raw = raw[:len(raw)-1]
	}
	return raw
}

type resendEvent struct {
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Data      struct {
		EmailID   string    `json:"email_id"`
		CreatedAt time.Time `json:"created_at"`
		Click     struct {
			Link      string    `json:"link"`
			Timestamp time.Time `json:"timestamp"`
		} `json:"click"`
	} `json:"data"`
}

func (e resendEvent) occurredAt(fallback time.Time) time.Time {
	if !e.Data.Click.Timestamp.IsZero() {
		return e.Data.Click.Timestamp.UTC()
	}
	if !e.Data.CreatedAt.IsZero() {
		return e.Data.CreatedAt.UTC()
	}
	if !e.CreatedAt.IsZero() {
		return e.CreatedAt.UTC()
	}
	if fallback.IsZero() {
		return time.Now().UTC()
	}
	return fallback.UTC()
}
