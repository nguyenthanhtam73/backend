package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserRepository persists and loads users. Implementations live in this package;
// usecases depend on this interface (ports) for testability.
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	UsernameExists(ctx context.Context, username string) (bool, error)
}

// GormUserRepository is the Postgres-backed UserRepository.
type GormUserRepository struct {
	db *gorm.DB
}

// NewUserRepository returns a UserRepository backed by GORM.
// Callers must pass a non-nil *gorm.DB; methods return errors if db is nil.
func NewUserRepository(db *gorm.DB) *GormUserRepository {
	return &GormUserRepository{db: db}
}

func (r *GormUserRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts a new user row.
func (r *GormUserRepository) Create(ctx context.Context, user *domain.User) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(user).Error
}

// GetByEmail loads a user by case-insensitive email match.
func (r *GormUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	email = strings.TrimSpace(strings.ToLower(email))
	var u domain.User
	tx := db.WithContext(ctx).Where("LOWER(email) = ?", email).First(&u)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &u, nil
}

// GetByID loads a user by primary key.
func (r *GormUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var u domain.User
	tx := db.WithContext(ctx).Where("id = ?", id).First(&u)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &u, nil
}

// UsernameExists returns true if any user has the exact username (case-sensitive as stored).
func (r *GormUserRepository) UsernameExists(ctx context.Context, username string) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	var count int64
	tx := db.WithContext(ctx).Model(&domain.User{}).Where("username = ?", username).Count(&count)
	if tx.Error != nil {
		return false, tx.Error
	}
	return count > 0, nil
}
