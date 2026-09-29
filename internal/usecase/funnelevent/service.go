// Package funnelevent persists first-party check-in funnel events.
package funnelevent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

// ErrUnavailable is returned when the service has no database.
var ErrUnavailable = errors.New("funnel event service unavailable")

// ValidationError is a client-input rejection. The message is safe to return.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil || e.Message == "" {
		return "invalid funnel event"
	}
	return e.Message
}

// Service records check-in funnel events.
type Service struct {
	events *repository.GormFunnelEventRepository
}

// NewService wires dependencies.
func NewService(events *repository.GormFunnelEventRepository) *Service {
	return &Service{events: events}
}

// Log validates and inserts one event.
// userID uuid.Nil stores a NULL user_id (guest). userAgent is trimmed, never required.
func (s *Service) Log(ctx context.Context, userID uuid.UUID, userAgent string, req dto.LogFunnelEventRequest) error {
	if s == nil || s.events == nil {
		return ErrUnavailable
	}
	row, msg := req.ValidateAndMap(userID)
	if row == nil {
		return &ValidationError{Message: msg}
	}
	row.UserAgent = trimUserAgent(userAgent)
	if err := s.events.Create(ctx, row); err != nil {
		slog.Error("funnel_event_insert_failed", "err", err)
		return err
	}
	return nil
}

func trimUserAgent(ua string) string {
	ua = strings.TrimSpace(ua)
	ua = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, ua)
	if utf8.RuneCountInString(ua) <= domain.MaxFunnelUserAgentRunes {
		return ua
	}
	return string([]rune(ua)[:domain.MaxFunnelUserAgentRunes])
}
