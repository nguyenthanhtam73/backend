package streak

import (
	"context"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// Store is the persistence port for streak rows (implemented by GormStreakRepository).
type Store interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Streak, error)
	UpdateAtomic(
		ctx context.Context,
		userID uuid.UUID,
		mutate func(row *domain.Streak) error,
	) (*domain.Streak, error)
}

// CheckDateSource supplies SkinCheck calendar days for streak reconcile / history.
type CheckDateSource interface {
	ListDistinctCheckDates(ctx context.Context, userID uuid.UUID) ([]time.Time, error)
	// FirstCheckDate is the earliest SkinCheck calendar day (UTC), or nil if none.
	FirstCheckDate(ctx context.Context, userID uuid.UUID) (*time.Time, error)
}
