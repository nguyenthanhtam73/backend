package auth

import (
	"context"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

// AuthRepository is the persistence port for AuthUsecase.
// Satisfied by repository.GormAuthRepository (and tests with fakes).
type AuthRepository = repository.AuthRepository

// TokenIssuer creates signed access and refresh JWTs for a subject user.
type TokenIssuer interface {
	SignAccess(userID uuid.UUID) (string, error)
	SignRefresh(userID uuid.UUID) (string, error)
	AccessTTL() time.Duration
}

// UserReaderWriter is retained as an alias for older call sites / tests.
// Prefer AuthRepository in new code.
type UserReaderWriter interface {
	Create(ctx context.Context, user *domain.User) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	UsernameExists(ctx context.Context, username string) (bool, error)
}
