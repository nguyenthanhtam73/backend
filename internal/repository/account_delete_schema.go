package repository

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
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
// A column that is already nullable is left alone, so a restart does not
// take ACCESS EXCLUSIVE on payment_orders or plan_change_logs.
// When an ALTER is required it runs inside a transaction with
// lock_timeout = 3s. A timeout is logged and returned; the process keeps
// serving and DELETE /me stays on the 503 guard until a later boot succeeds.
// Non-Postgres drivers are a no-op (tests already create nullable columns).
func ApplyAccountDeletionSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	altered := 0
	for _, col := range accountDeletionColumns {
		if !safeIdent(col.table) || !safeIdent(col.column) {
			return fmt.Errorf("unsafe identifier")
		}
		nullable, err := columnNullable(db, col.table, col.column)
		if err != nil {
			return fmt.Errorf("migration 024: %s.%s: %w", col.table, col.column, err)
		}
		if nullable {
			continue
		}
		if err := dropNotNull(db, col.table, col.column); err != nil {
			if isLockTimeout(err) {
				slog.Error("account deletion schema: lock timeout; DELETE /me stays unavailable",
					"table", col.table,
					"column", col.column,
					"error", err.Error(),
				)
				return fmt.Errorf("migration 024: %s.%s: lock timeout: %w", col.table, col.column, err)
			}
			return fmt.Errorf("migration 024: %s.%s: %w", col.table, col.column, err)
		}
		altered++
	}
	if altered == 0 {
		slog.Info("account deletion schema: columns already nullable")
	}
	return nil
}

func dropNotNull(db *gorm.DB, table, column string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout = '3s'").Error; err != nil {
			return err
		}
		q := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", table, column)
		return tx.Exec(q).Error
	})
}

func isLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "lock timeout") || strings.Contains(msg, "55p03")
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
