package repository

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupStreakAtRiskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Unique DSN per test so parallel packages don't share tables.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := db.AutoMigrate(&domain.Streak{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func ptrDate(t time.Time) *time.Time { return &t }

func insertStreak(t *testing.T, db *gorm.DB, row *domain.Streak) {
	t.Helper()
	// Map Create: GORM's `default:1` on FreezesAvailable replaces struct zero
	// with 1 even when Select(...FreezesAvailable) is used.
	now := time.Now().UTC()
	err := db.Model(&domain.Streak{}).Create(map[string]interface{}{
		"user_id":            row.UserID,
		"current_streak":     row.CurrentStreak,
		"longest_streak":     row.LongestStreak,
		"last_check_in_date": row.LastCheckInDate,
		"protected_until":    row.ProtectedUntil,
		"last_freeze_date":   row.LastFreezeDate,
		"freeze_dates":       row.FreezeDates,
		"freezes_available":  row.FreezesAvailable,
		"created_at":         now,
		"updated_at":         now,
	}).Error
	if err != nil {
		t.Fatalf("create: %v", err)
	}
}

func TestListUsersAtRisk_MatchesEvaluateStreakView(t *testing.T) {
	today := streaktime.Today()
	yesterday := today.AddDate(0, 0, -1)
	day2 := today.AddDate(0, 0, -2)
	day3 := today.AddDate(0, 0, -3)

	cases := []struct {
		name     string
		row      domain.Streak
		wantRisk bool
	}{
		{
			name: "days_since_1 unprotected",
			row: domain.Streak{
				UserID:          uuid.New(),
				CurrentStreak:   5,
				LastCheckInDate: ptrDate(yesterday),
			},
			wantRisk: true,
		},
		{
			name: "days_since_1 protected today",
			row: domain.Streak{
				UserID:          uuid.New(),
				CurrentStreak:   5,
				LastCheckInDate: ptrDate(yesterday),
				ProtectedUntil:  ptrDate(today),
			},
			wantRisk: false,
		},
		{
			name: "days_since_2 freeze bridge",
			row: domain.Streak{
				UserID:          uuid.New(),
				CurrentStreak:   4,
				LastCheckInDate: ptrDate(day2),
				ProtectedUntil:  ptrDate(yesterday),
			},
			wantRisk: true,
		},
		{
			name: "days_since_2 pending auto freeze",
			row: domain.Streak{
				UserID:           uuid.New(),
				CurrentStreak:    4,
				LastCheckInDate:  ptrDate(day2),
				FreezesAvailable: 1,
			},
			wantRisk: true,
		},
		{
			name: "days_since_2 soft expired",
			row: domain.Streak{
				UserID:           uuid.New(),
				CurrentStreak:    4,
				LastCheckInDate:  ptrDate(day2),
				FreezesAvailable: 0,
			},
			wantRisk: false,
		},
		{
			name: "checked in today",
			row: domain.Streak{
				UserID:          uuid.New(),
				CurrentStreak:   3,
				LastCheckInDate: ptrDate(today),
			},
			wantRisk: false,
		},
		{
			name: "gap too large",
			row: domain.Streak{
				UserID:           uuid.New(),
				CurrentStreak:    3,
				LastCheckInDate:  ptrDate(day3),
				FreezesAvailable: 0,
			},
			wantRisk: false,
		},
		{
			name: "zero streak",
			row: domain.Streak{
				UserID:          uuid.New(),
				CurrentStreak:   0,
				LastCheckInDate: ptrDate(yesterday),
			},
			wantRisk: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupStreakAtRiskTestDB(t)
			repo := NewStreakRepository(db)
			ctx := context.Background()

			view := dto.EvaluateStreakView(&tc.row, today)
			if view.IsAtRisk != tc.wantRisk {
				t.Fatalf("EvaluateStreakView IsAtRisk=%v want %v", view.IsAtRisk, tc.wantRisk)
			}

			insertStreak(t, db, &tc.row)

			got, err := repo.ListUsersAtRisk(ctx)
			if err != nil {
				t.Fatalf("ListUsersAtRisk: %v", err)
			}
			found := false
			for _, id := range got {
				if id == tc.row.UserID {
					found = true
					break
				}
			}
			if found != tc.wantRisk {
				// Help diagnose sqlite date / freeze quirks.
				var stored domain.Streak
				_ = db.Where("user_id = ?", tc.row.UserID).First(&stored).Error
				t.Fatalf("in ListUsersAtRisk=%v want %v (stored freezes=%d last=%v pu=%v)",
					found, tc.wantRisk, stored.FreezesAvailable, stored.LastCheckInDate, stored.ProtectedUntil)
			}
		})
	}
}
