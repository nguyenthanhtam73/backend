package repository

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// ErrAccountDeletionSchema means payment_orders.user_id or the plan-change
// user id columns are still NOT NULL. DELETE /me must stop before it opens
// a transaction so a failed migration cannot leave a half-deleted account.
var ErrAccountDeletionSchema = errors.New("account deletion schema is not ready")

// accountDeletionColumns are the NOT NULL columns migration 024 drops.
// AutoMigrate never changes nullability of an existing column.
var accountDeletionColumns = []struct {
	table  string
	column string
}{
	{"payment_orders", "user_id"},
	{"plan_change_logs", "user_id"},
	{"plan_change_logs", "actor_user_id"},
}

// ApplyAccountDeletionSchema runs migration 024. It is idempotent.
// Postgres takes ACCESS EXCLUSIVE for each ALTER, so call this once at
// process startup before the HTTP server accepts traffic.
// Non-Postgres drivers are a no-op (tests already create nullable columns).
func ApplyAccountDeletionSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	for _, col := range accountDeletionColumns {
		if !safeIdent(col.table) || !safeIdent(col.column) {
			return fmt.Errorf("unsafe identifier")
		}
		q := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", col.table, col.column)
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("migration 024: %s.%s: %w", col.table, col.column, err)
		}
	}
	return nil
}

// accountDeletionSchemaReady is a read of information_schema. It does not
// take ACCESS EXCLUSIVE. SQLite skips the check.
func accountDeletionSchemaReady(db *gorm.DB) error {
	if db == nil || db.Dialector.Name() != "postgres" {
		return nil
	}
	for _, col := range accountDeletionColumns {
		ok, err := columnNullable(db, col.table, col.column)
		if err != nil {
			return fmt.Errorf("%w: %s.%s: %v", ErrAccountDeletionSchema, col.table, col.column, err)
		}
		if !ok {
			return fmt.Errorf("%w: %s.%s is not nullable", ErrAccountDeletionSchema, col.table, col.column)
		}
	}
	return nil
}

func columnNullable(db *gorm.DB, table, column string) (bool, error) {
	var row struct {
		IsNullable string `gorm:"column:is_nullable"`
	}
	err := db.Raw(`
		SELECT is_nullable
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = ?
		  AND column_name = ?
	`, table, column).Scan(&row).Error
	if err != nil {
		return false, err
	}
	if row.IsNullable == "" {
		return false, fmt.Errorf("column missing")
	}
	return strings.EqualFold(row.IsNullable, "YES"), nil
}

func safeIdent(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}
