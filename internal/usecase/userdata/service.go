// Package userdata handles destructive privacy actions (delete all personal data).
package userdata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/internal/storage"
	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("user data service unavailable")
	ErrInvalidUser = errors.New("invalid user id")
)

// Service wipes diary data while keeping the auth account.
type Service struct {
	repo  *repository.UserDataRepository
	store storage.Storage
	cache *ai.MemoryCache
}

// NewService wires dependencies. cache may be nil.
func NewService(
	repo *repository.UserDataRepository,
	store storage.Storage,
	cache *ai.MemoryCache,
) *Service {
	return &Service{repo: repo, store: store, cache: cache}
}

// DeleteAll removes personal skincare data for userID.
func (s *Service) DeleteAll(ctx context.Context, userID uuid.UUID) (dto.DeleteUserDataResponse, error) {
	var zero dto.DeleteUserDataResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, ErrInvalidUser
	}
	if err := s.repo.DeleteAllPersonalData(ctx, userID); err != nil {
		return zero, fmt.Errorf("delete user data: %w", err)
	}
	// Best-effort: remove the user's stored photos (disk or R2). A storage error
	// must not block the DB wipe from being reported as successful, since the
	// personal rows are already gone — just log for follow-up.
	if s.store != nil {
		if err := s.store.DeletePrefix(ctx, userID.String()+"/"); err != nil {
			slog.Warn("user-data: delete stored photos failed", "user_id", userID, "err", err)
		}
	}
	if s.cache != nil {
		s.cache.Bust(userID)
	}
	return dto.DeleteUserDataResponse{
		DeletedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}
