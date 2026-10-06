package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PushClickRepository stores client-reported notification clicks.
type PushClickRepository struct {
	db *gorm.DB
}

// NewPushClickRepository constructs the repository.
func NewPushClickRepository(db *gorm.DB) *PushClickRepository {
	return &PushClickRepository{db: db}
}

func (r *PushClickRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// InsertIfNew records a click. inserted is false when this user already
// stored the same idempotency key.
func (r *PushClickRepository) InsertIfNew(
	ctx context.Context,
	ev *domain.PushClickEvent,
) (bool, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return false, err
	}
	if ev == nil || ev.UserID == uuid.Nil || strings.TrimSpace(ev.IdempotencyKey) == "" {
		return false, fmt.Errorf("user id and idempotency key required")
	}
	ev.IdempotencyKey = strings.TrimSpace(ev.IdempotencyKey)
	tx := db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "idempotency_key"},
			},
			DoNothing: true,
		}).
		Create(ev)
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}
