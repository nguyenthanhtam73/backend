package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MascotRepository persists the per-user mascot and the item catalog.
type MascotRepository struct {
	db *gorm.DB
}

// NewMascotRepository returns a mascot repository.
func NewMascotRepository(db *gorm.DB) *MascotRepository {
	return &MascotRepository{db: db}
}

func (r *MascotRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// EnsureCatalog inserts free starter items that are not already present.
func (r *MascotRepository) EnsureCatalog(ctx context.Context) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return SeedMascotCatalog(db.WithContext(ctx))
}

// SeedMascotCatalog inserts missing free starter items. Safe to call on every boot.
func SeedMascotCatalog(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	inserted := 0
	for _, item := range domain.DefaultMascotItems() {
		var n int64
		if err := db.Model(&domain.MascotItem{}).Where("id = ?", item.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		row := item
		if err := db.Create(&row).Error; err != nil {
			if IsUniqueConflict(err) {
				continue
			}
			return err
		}
		inserted++
	}
	if inserted > 0 {
		slog.Info("mascot catalog: seeded default items", "count", inserted)
	}
	return nil
}

// ListItems returns the catalog.
func (r *MascotRepository) ListItems(ctx context.Context) ([]domain.MascotItem, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var rows []domain.MascotItem
	if err := db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Get returns the user's mascot, or nil when they have not opened it yet.
func (r *MascotRepository) Get(ctx context.Context, userID uuid.UUID) (*domain.UserMascot, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	var row domain.UserMascot
	err = db.WithContext(ctx).Where("user_id = ?", userID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Create inserts the first mascot row.
func (r *MascotRepository) Create(ctx context.Context, row *domain.UserMascot) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if row == nil || row.UserID == uuid.Nil {
		return fmt.Errorf("mascot required")
	}
	return db.WithContext(ctx).Omit("User").Create(row).Error
}

// SaveLoadout replaces the JSON look.
func (r *MascotRepository) SaveLoadout(ctx context.Context, userID uuid.UUID, raw json.RawMessage) error {
	row, err := r.Get(ctx, userID)
	if err != nil {
		return err
	}
	if row == nil {
		return gorm.ErrRecordNotFound
	}
	row.Loadout = raw
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Omit("User").Save(row).Error
}
