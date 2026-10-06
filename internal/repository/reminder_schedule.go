package repository

import (
	"github.com/dadiary/backend/internal/domain"
	"gorm.io/gorm"
)

// reminderDisabledUserIDs selects accounts that turned reminders off.
// NULL (never set) and true are not in this set. Hard-deleted users are not
// in it either, so a missing row keeps the caller's existing skip path.
// GORM adds users.deleted_at IS NULL, so a soft-deleted account is not excluded
// here; GetByID already drops those rows.
func reminderDisabledUserIDs(db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{NewDB: true}).
		Model(&domain.User{}).
		Select("id").
		Where("reminder_enabled = ?", false)
}
