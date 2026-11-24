package repository

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCoachCopyAudit_NullEmailIsCounted(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:coach_audit_null_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE users (id text primary key, email text, deleted_at datetime)`,
		`CREATE TABLE skin_checks (
			id text primary key, user_id text, client_kind text, created_at datetime,
			deleted_at datetime, check_date datetime, image_urls text, visibility text)`,
		`CREATE TABLE skin_analyses (
			id text primary key, skin_check_id text, status text, summary_notes text,
			strengths text, improvements text, routine_hints text, avoid_or_patch text,
			safety_flags text, skin_scores text, deleted_at datetime)`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	userID := uuid.NewString()
	checkID := uuid.NewString()
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO users (id, email) VALUES (?, NULL)`, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO skin_checks (id, user_id, client_kind, created_at, check_date, image_urls, visibility) VALUES (?, ?, 'web', ?, ?, '[]', 'private')`,
		checkID, userID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO skin_analyses (id, skin_check_id, status, summary_notes) VALUES (?, ?, 'completed', 'da ổn')`,
		uuid.NewString(), checkID).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewCoachCopyAudit(db)
	counts, err := repo.CountCompletedByClient(context.Background(), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0].ClientKind != "web" || counts[0].ChecksCompleted != 1 || counts[0].DistinctUsers != 1 {
		t.Fatalf("null email was dropped: %#v", counts)
	}
}
