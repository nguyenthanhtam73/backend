package repository

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// ErrShelfCapExceeded is returned when a Free wardrobe insert would exceed the shelf cap.
var ErrShelfCapExceeded = errors.New("wardrobe shelf cap exceeded")

// IsUniqueViolation reports whether err is a Postgres unique constraint violation (SQLSTATE 23505).
// Used after INSERT to map race conditions on email/username to domain-level errors.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsUniqueConflict reports a unique-key collision on Postgres or SQLite (tests).
func IsUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || IsUniqueViolation(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "violates unique constraint")
}
