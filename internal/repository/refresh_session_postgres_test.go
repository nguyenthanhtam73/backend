package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRefreshSessionClientKindDefault_Postgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("DADIARY_DATABASE_URL"))
	if dsn == "" {
		t.Skip("DADIARY_DATABASE_URL not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&domain.RefreshSession{}); err != nil {
		t.Fatal(err)
	}

	id := uuid.New()
	userID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	// Omit client_kind so the column default fills existing-style rows.
	err = db.Exec(`
		INSERT INTO refresh_sessions (id, user_id, token_hash, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, strings.Repeat("ab", 32), now.Add(time.Hour), now, now,
	).Error
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Unscoped().Where("id = ?", id).Delete(&domain.RefreshSession{}).Error
	})

	var row domain.RefreshSession
	if err := db.First(&row, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if row.ClientKind != domain.RefreshClientWeb {
		t.Fatalf("default client_kind=%q", row.ClientKind)
	}

	android := &domain.RefreshSession{
		ID:         uuid.New(),
		UserID:     userID,
		TokenHash:  strings.Repeat("cd", 32),
		ExpiresAt:  now.Add(90 * 24 * time.Hour),
		ClientKind: domain.RefreshClientAndroid,
	}
	if err := NewRefreshSessionRepository(db).Create(context.Background(), android); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Unscoped().Where("id = ?", android.ID).Delete(&domain.RefreshSession{}).Error
	})
	got, err := NewRefreshSessionRepository(db).GetByID(context.Background(), android.ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("stored client_kind=%q", got.ClientKind)
	}
}
