package repository

import (
	"context"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuthRepository is the persistence port for auth flows (Clean Architecture).
// Implemented by the GORM user store — kept separate from generic UserRepository
// naming so AuthUsecase depends on an auth-scoped contract.
type AuthRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	UsernameExists(ctx context.Context, username string) (bool, error)
}

// GormAuthRepository adapts GormUserRepository to AuthRepository.
type GormAuthRepository struct {
	users *GormUserRepository
}

// NewAuthRepository wires the auth persistence adapter.
func NewAuthRepository(db *gorm.DB) *GormAuthRepository {
	return &GormAuthRepository{users: NewUserRepository(db)}
}

// NewAuthRepositoryFromUsers wraps an existing user repo (tests / shared wiring).
func NewAuthRepositoryFromUsers(users *GormUserRepository) *GormAuthRepository {
	return &GormAuthRepository{users: users}
}

func (r *GormAuthRepository) Create(ctx context.Context, user *domain.User) error {
	if r == nil || r.users == nil {
		return gorm.ErrInvalidDB
	}
	return r.users.Create(ctx, user)
}

func (r *GormAuthRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	if r == nil || r.users == nil {
		return nil, gorm.ErrInvalidDB
	}
	return r.users.GetByEmail(ctx, email)
}

func (r *GormAuthRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if r == nil || r.users == nil {
		return nil, gorm.ErrInvalidDB
	}
	return r.users.GetByID(ctx, id)
}

func (r *GormAuthRepository) UsernameExists(ctx context.Context, username string) (bool, error) {
	if r == nil || r.users == nil {
		return false, gorm.ErrInvalidDB
	}
	return r.users.UsernameExists(ctx, username)
}

// Users exposes the underlying user repository for modules that need the wider API.
func (r *GormAuthRepository) Users() *GormUserRepository {
	if r == nil {
		return nil
	}
	return r.users
}
