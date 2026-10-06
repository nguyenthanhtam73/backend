package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSkinCheckClientKindDefault_SQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:skin_client_kind_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinCheck{}); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	userID := uuid.New()
	now := time.Now().UTC()
	// Omit client_kind so the column default fills an existing-style row.
	err = db.Exec(`
		INSERT INTO skin_checks (id, user_id, image_urls, check_date, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, `[]`, now, now, now,
	).Error
	if err != nil {
		t.Fatal(err)
	}
	if got := clientKindColumn(t, db, id); got != domain.RefreshClientWeb {
		t.Fatalf("default client_kind=%q", got)
	}

	// Adding the column onto a table that already has rows must not fail,
	// and those rows must read as web.
	if err := db.Exec(`ALTER TABLE skin_checks DROP COLUMN client_kind`).Error; err != nil {
		t.Fatal(err)
	}
	legacyID := uuid.New()
	err = db.Exec(`
		INSERT INTO skin_checks (id, user_id, image_urls, check_date, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		legacyID, userID, `[]`, now, now, now,
	).Error
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinCheck{}); err != nil {
		t.Fatal(err)
	}
	if got := clientKindColumn(t, db, legacyID); got != domain.RefreshClientWeb {
		t.Fatalf("backfilled client_kind=%q", got)
	}
}

func clientKindColumn(t *testing.T, db *gorm.DB, id uuid.UUID) string {
	t.Helper()
	var kind string
	if err := db.Raw(`SELECT client_kind FROM skin_checks WHERE id = ?`, id).Scan(&kind).Error; err != nil {
		t.Fatal(err)
	}
	return kind
}

func TestSkinCheckClientKindDefault_Postgres(t *testing.T) {
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

	if err := db.AutoMigrate(&domain.User{}, &domain.SkinCheck{}); err != nil {
		t.Fatal(err)
	}
	user := &domain.User{
		ID:       uuid.New(),
		Email:    "client-kind-" + uuid.NewString() + "@example.test",
		Username: "ck" + uuid.NewString()[:8],
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Unscoped().Where("user_id = ?", user.ID).Delete(&domain.SkinCheck{}).Error
		_ = db.Unscoped().Where("id = ?", user.ID).Delete(&domain.User{}).Error
	})

	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	err = db.Exec(`
		INSERT INTO skin_checks (id, user_id, image_urls, check_date, created_at, updated_at)
		VALUES (?, ?, CAST(? AS jsonb), ?, ?, ?)`,
		id, user.ID, `[]`, now, now, now,
	).Error
	if err != nil {
		t.Fatal(err)
	}
	var row domain.SkinCheck
	if err := db.First(&row, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if row.ClientKind != domain.RefreshClientWeb {
		t.Fatalf("default client_kind=%q", row.ClientKind)
	}

	stored := &domain.SkinCheck{
		UserID:     user.ID,
		ImageURLs:  []byte(`[]`),
		CheckDate:  now,
		Visibility: domain.CheckVisibilityPrivate,
		ClientKind: domain.RefreshClientAndroid,
	}
	if err := NewSkinCheckRepository(db).CreateWithAnalysis(context.Background(), stored, &domain.SkinAnalysis{
		Status: domain.AnalysisStatusPending,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := NewSkinCheckRepository(db).GetByID(context.Background(), stored.ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("stored client_kind=%q", got.ClientKind)
	}
}
