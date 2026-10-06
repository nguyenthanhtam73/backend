// Package pushclick records notification clicks reported by the client.
package pushclick

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

// ErrUnavailable means the store is not wired.
var ErrUnavailable = errors.New("push click service unavailable")

// Input is one notification click from the app or service worker.
type Input struct {
	Kind           string
	Tag            string
	IdempotencyKey string
	ClickedAt      time.Time
}

// Result reports whether a new row was written.
type Result struct {
	Recorded  bool `json:"recorded"`
	Duplicate bool `json:"duplicate"`
}

// Service persists push clicks for the authenticated user.
type Service struct {
	repo *repository.PushClickRepository
	now  func() time.Time
}

// NewService constructs the service.
func NewService(repo *repository.PushClickRepository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// Record stores one click. A repeated idempotency key for the same user is
// a success with Duplicate set (no second row).
func (s *Service) Record(ctx context.Context, userID uuid.UUID, in Input) (Result, error) {
	var zero Result
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, errors.New("user id required")
	}
	now := time.Now().UTC()
	if s.now != nil {
		now = s.now().UTC()
	}
	clicked := in.ClickedAt.UTC()
	if clicked.IsZero() || clicked.After(now.Add(5*time.Minute)) || clicked.Before(now.Add(-7*24*time.Hour)) {
		clicked = now
	}
	kind := clip(in.Kind, 64)
	tag := clip(in.Tag, 128)
	key := clip(in.IdempotencyKey, 128)
	if key == "" {
		key = kind + "|" + tag + "|" + clicked.Truncate(time.Minute).Format(time.RFC3339)
	}
	ev := &domain.PushClickEvent{
		UserID:           userID,
		NotificationKind: kind,
		Tag:              tag,
		IdempotencyKey:   key,
		ClickedAt:        clicked,
	}
	inserted, err := s.repo.InsertIfNew(ctx, ev)
	if err != nil {
		return zero, err
	}
	if !inserted {
		return Result{Duplicate: true}, nil
	}
	return Result{Recorded: true}, nil
}

func clip(raw string, maxRunes int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(raw) <= maxRunes {
		return raw
	}
	runes := []rune(raw)
	return string(runes[:maxRunes])
}
