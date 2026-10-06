package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPhotoContextMigration_AutoMigrateTwice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:photo_ctx_mig_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinCheck{}); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinCheck{}); err != nil {
		t.Fatal(err)
	}
	row := &domain.SkinCheck{
		UserID: uuid.New(), Visibility: domain.CheckVisibilityPrivate,
		ImageURLs:    []byte(`[]`),
		PhotoContext: []byte(`{"images":[{"index":0,"kind":"closeup","zone":"nose"}]}`),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.Raw(`SELECT photo_context FROM skin_checks WHERE id = ?`, row.ID).Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "nose") {
		t.Fatalf("column round-trip %q", got)
	}
	up, err := os.ReadFile("../../migrations/028_skin_check_photo_context.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/028_skin_check_photo_context.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(up), "ADD COLUMN IF NOT EXISTS photo_context") {
		t.Fatalf("up sql not idempotent:\n%s", up)
	}
	if !strings.Contains(string(down), "DROP COLUMN IF EXISTS photo_context") {
		t.Fatalf("down sql:\n%s", down)
	}
}

func TestPhotoContextSQL_IdempotentOnPostgres(t *testing.T) {
	db := openAccountDeletePostgres(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/028_skin_check_photo_context.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := strings.TrimSpace(string(up))
	for i := 0; i < 2; i++ {
		if err := db.Exec(sqlText).Error; err != nil {
			t.Fatalf("apply 028 pass %d: %v", i+1, err)
		}
	}
	if err := db.Exec(`ALTER TABLE skin_checks DROP COLUMN IF EXISTS photo_context`).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Exec(sqlText).Error; err != nil {
			t.Fatalf("re-add 028 pass %d: %v", i+1, err)
		}
	}
}
