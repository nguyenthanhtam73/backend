// Package userdata handles privacy actions (export dump + delete all personal data).
package userdata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/internal/storage"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("user data service unavailable")
	ErrInvalidUser = errors.New("invalid user id")
)

// Service wipes diary data while keeping the auth account.
type Service struct {
	repo    *repository.UserDataRepository
	store   storage.Storage
	cache   *ai.MemoryCache
	premium *premiumuc.Service
}

// NewService wires dependencies. cache / premium may be nil.
func NewService(
	repo *repository.UserDataRepository,
	store storage.Storage,
	cache *ai.MemoryCache,
	premium *premiumuc.Service,
) *Service {
	return &Service{repo: repo, store: store, cache: cache, premium: premium}
}

// Export returns a portable diary dump. Requires FeatureExportData (Premium+).
func (s *Service) Export(ctx context.Context, userID uuid.UUID) (dto.ExportUserDataResponse, error) {
	var zero dto.ExportUserDataResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, ErrInvalidUser
	}
	if s.premium == nil {
		return zero, ErrUnavailable
	}
	if err := s.premium.AssertFeature(ctx, userID, domain.FeatureExportData); err != nil {
		return zero, err
	}
	out, err := s.repo.ExportBundle(ctx, userID)
	if err != nil {
		return zero, fmt.Errorf("export user data: %w", err)
	}
	tier, _ := s.premium.PlanTier(ctx, userID)
	out.PlanTier = string(tier)
	out.ExportedAt = time.Now().UTC().Format(time.RFC3339)
	slog.Info("user-data: export ok",
		"user_id", userID.String(),
		"checks", len(out.SkinChecks),
		"routines", len(out.Routines),
		"wardrobe", len(out.Wardrobe),
	)
	return out, nil
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
