package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

// EnsureFreeGrants inserts a permanent free grant for each starter the user
// does not already hold. Existing rows are left alone, including expired ones.
func (r *MascotRepository) EnsureFreeGrants(ctx context.Context, userID uuid.UUID, now time.Time) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for _, item := range domain.DefaultMascotItems() {
		if !item.IsFree {
			continue
		}
		var n int64
		err := db.WithContext(ctx).Model(&domain.UserMascotItem{}).
			Where("user_id = ? AND item_id = ?", userID, item.ID).
			Count(&n).Error
		if err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		row := domain.UserMascotItem{
			UserID:     userID,
			ItemID:     item.ID,
			Source:     domain.MascotSourceFree,
			AcquiredAt: now.UTC(),
		}
		if err := db.WithContext(ctx).Omit("User", "Item").Create(&row).Error; err != nil {
			if IsUniqueConflict(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// ActiveItemSources returns grants that have not expired, keyed by item id.
// The value is the grant source (free, shop, …). expires_at NULL is permanent.
// Expired rows stay in the table; callers just skip them.
func (r *MascotRepository) ActiveItemSources(ctx context.Context, userID uuid.UUID, now time.Time) (map[string]string, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var rows []domain.UserMascotItem
	err = db.WithContext(ctx).
		Select("item_id", "source").
		Where("user_id = ? AND (expires_at IS NULL OR expires_at > ?)", userID, now.UTC()).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.ItemID] = row.Source
	}
	return out, nil
}

// SaveLoadout replaces the JSON look without writing the glow columns.
func (r *MascotRepository) SaveLoadout(ctx context.Context, userID uuid.UUID, raw json.RawMessage) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}
	res := db.WithContext(ctx).Model(&domain.UserMascot{}).
		Where("user_id = ?", userID).
		Update("loadout", raw)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SaveGlowLevel raises the stored high water. A lower value is not written.
func (r *MascotRepository) SaveGlowLevel(ctx context.Context, userID uuid.UUID, level int) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}
	if level < 1 {
		level = 1
	}
	res := db.WithContext(ctx).Model(&domain.UserMascot{}).
		Where("user_id = ? AND glow_level < ?", userID, level).
		Update("glow_level", level)
	return res.Error
}

// SaveGlowSeen records the level the client just acknowledged.
func (r *MascotRepository) SaveGlowSeen(ctx context.Context, userID uuid.UUID, seen int) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}
	if seen < 1 {
		seen = 1
	}
	return db.WithContext(ctx).Model(&domain.UserMascot{}).
		Where("user_id = ?", userID).
		Update("glow_level_seen", seen).Error
}
