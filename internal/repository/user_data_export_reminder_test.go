package repository

import (
	"context"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestExportBundle_IncludesReminderSettings(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:export_reminder_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
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
	users := NewUserRepository(db)
	unset := &domain.User{Email: "export-unset@test.com", Username: "export-unset@test.com", IsActive: true}
	set := &domain.User{Email: "export-set@test.com", Username: "export-set@test.com", IsActive: true}
	if err := users.Create(context.Background(), unset); err != nil {
		t.Fatal(err)
	}
	if err := users.Create(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	enabled := false
	hhmm := "21:15"
	tz := "America/Los_Angeles"
	if err := users.SetReminderSchedule(context.Background(), set.ID, &enabled, &hhmm, &tz); err != nil {
		t.Fatal(err)
	}

	repo := NewUserDataRepository(db)
	blank, err := repo.ExportBundle(context.Background(), unset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blank.Reminder.Enabled != nil || blank.Reminder.Time != nil || blank.Reminder.Timezone != nil {
		t.Fatalf("unset export: %+v", blank.Reminder)
	}

	got, err := repo.ExportBundle(context.Background(), set.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reminder.Enabled == nil || *got.Reminder.Enabled {
		t.Fatalf("enabled: %+v", got.Reminder.Enabled)
	}
	if got.Reminder.Time == nil || *got.Reminder.Time != "21:15" {
		t.Fatalf("time: %+v", got.Reminder.Time)
	}
	if got.Reminder.Timezone == nil || *got.Reminder.Timezone != "America/Los_Angeles" {
		t.Fatalf("timezone: %+v", got.Reminder.Timezone)
	}
}
