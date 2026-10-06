package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestExportBundle_IncludesPhotoContext(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:export_photo_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.SkinProfile{},
		&domain.Streak{},
		&domain.SkinCheck{},
		&domain.SkinAnalysis{},
		&domain.RoutineEntry{},
		&domain.SkincareProduct{},
	); err != nil {
		t.Fatal(err)
	}
	user := &domain.User{Email: "export-photo@test.com", Username: "export-photo@test.com", IsActive: true}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	with := &domain.SkinCheck{
		UserID: user.ID, CheckDate: when, Visibility: domain.CheckVisibilityPrivate,
		ImageURLs:    json.RawMessage(`["checks/a.jpg"]`),
		PhotoContext: json.RawMessage(`{"images":[{"index":0,"kind":"closeup","zone":"chin"}],"skin_context":{"firmness":"soft","pain":"itchy"}}`),
	}
	without := &domain.SkinCheck{
		UserID: user.ID, CheckDate: when.AddDate(0, 0, -1), Visibility: domain.CheckVisibilityPrivate,
		ImageURLs: json.RawMessage(`["checks/b.jpg"]`),
	}
	if err := db.Create(with).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(without).Error; err != nil {
		t.Fatal(err)
	}
	got, err := NewUserDataRepository(db).ExportBundle(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SkinChecks) != 2 {
		t.Fatalf("checks %d", len(got.SkinChecks))
	}
	raw, err := json.Marshal(got.SkinChecks)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	var saw, sawOld bool
	for _, row := range rows {
		ctx, ok := row["photo_context"]
		if row["id"] == with.ID.String() {
			saw = true
			if !ok {
				t.Fatalf("new check missing photo_context: %s", raw)
			}
			blob, _ := json.Marshal(ctx)
			if !json.Valid(blob) || !containsAll(string(blob), "chin", "soft", "itchy") {
				t.Fatalf("photo_context %s", blob)
			}
		}
		if row["id"] == without.ID.String() {
			sawOld = true
			if ok {
				t.Fatalf("old check should omit photo_context: %s", raw)
			}
		}
	}
	if !saw || !sawOld {
		t.Fatalf("rows %s", raw)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !containsStr(s, p) {
			return false
		}
	}
	return true
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}
