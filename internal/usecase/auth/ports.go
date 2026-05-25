package auth

import (
	"context"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// UserReaderWriter abstracts persistence for auth flows.
// Kept small on purpose; outfit and other modules can introduce wider ports later.
type UserReaderWriter interface {
	Create(ctx context.Context, user *domain.User) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	UsernameExists(ctx context.Context, username string) (bool, error)
}

// TokenIssuer creates signed access and refresh JWTs for a subject user.
type TokenIssuer interface {
	SignAccess(userID uuid.UUID) (string, error)
	SignRefresh(userID uuid.UUID) (string, error)
	AccessTTL() time.Duration
}
