// Package aifeedback records user thumbs-up/down on AI outputs for future prompt iteration / eval datasets.
package aifeedback

import (
	"context"
	"errors"
	"fmt"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/google/uuid"
)

var ErrUnavailable = errors.New("feedback service unavailable")

// Service persists AI feedback rows.
//
// `cache` is the shared in-process memory cache. When non-nil it is busted
// after every Create so the very next AI call reflects the new vote in the
// USER_FEEDBACK_HISTORY section of the memory block (without waiting for
// the TTL).
type Service struct {
	repo  *repository.GormAIFeedbackRepository
	cache *ai.MemoryCache
}

// NewService constructs Service. cache may be nil — in that case feedback
// writes still succeed, they just don't pro-actively invalidate cached
// memory entries (TTL expiry catches up within ~5 minutes).
func NewService(repo *repository.GormAIFeedbackRepository, cache *ai.MemoryCache) *Service {
	return &Service{repo: repo, cache: cache}
}

// Create stores one feedback event.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, req dto.CreateAIFeedbackRequest) (dto.AIFeedbackResponse, error) {
	var zero dto.AIFeedbackResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	row, msg := req.ValidateAndMap(userID)
	if row == nil {
		return zero, fmt.Errorf("%s", msg)
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return zero, err
	}
	// New vote → invalidate the memory cache so the next AI call sees it
	// in USER_FEEDBACK_HISTORY immediately rather than waiting for TTL.
	s.cache.Bust(userID)
	return dto.AIFeedbackResponse{
		ID:      row.ID.String(),
		Message: "feedback_recorded",
	}, nil
}

// ListMine returns the most recent feedback rows for userID, newest first.
//
// `limit` is bound-checked at the repo layer (defaults to 50, max 200) so
// callers can safely pass 0 to mean "use the default page".
func (s *Service) ListMine(ctx context.Context, userID uuid.UUID, limit int) (dto.AIFeedbackHistoryResponse, error) {
	var zero dto.AIFeedbackHistoryResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	rows, err := s.repo.ListByUser(ctx, userID, limit)
	if err != nil {
		return zero, err
	}
	out := dto.AIFeedbackHistoryResponse{
		Items: make([]dto.AIFeedbackHistoryItem, 0, len(rows)),
		Count: len(rows),
	}
	for _, r := range rows {
		out.Items = append(out.Items, dto.FromDomainFeedback(r))
	}
	return out, nil
}
