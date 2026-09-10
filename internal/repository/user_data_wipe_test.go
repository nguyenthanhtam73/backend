package repository

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeleteAllPersonalData_WipesStreakAndUsage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:wipe_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.SkinCheck{},
		&domain.SkinAnalysis{},
		&domain.SkinProfile{},
		&domain.RoutineEntry{},
		&domain.SkincareProduct{},
		&domain.AIUserFeedback{},
		&domain.Feedback{},
		&domain.AffiliateClick{},
		&domain.ProgressLog{},
		&domain.PushSubscription{},
		&domain.PushSendReceipt{},
		&domain.CheckInReminderFlag{},
		&domain.RoutineSuggestJob{},
		&domain.Streak{},
		&domain.UserUsage{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	user := &domain.User{Email: "wipe@dadiary.test", Username: "wipe_user", IsActive: true}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("user: %v", err)
	}
	start, end, key := domain.CurrentUTCMonthPeriod(time.Now())
	if err := db.Create(&domain.Streak{UserID: user.ID, CurrentStreak: 4, LongestStreak: 4}).Error; err != nil {
		t.Fatalf("streak: %v", err)
	}
	if err := db.Create(&domain.UserUsage{
		UserID:      user.ID,
		FeatureKey:  string(domain.FeatureAIRoutineSuggestion),
		UsageCount:  2,
		PeriodKey:   key,
		PeriodStart: start,
		PeriodEnd:   end,
	}).Error; err != nil {
		t.Fatalf("usage: %v", err)
	}

	repo := NewUserDataRepository(db)
	if err := repo.DeleteAllPersonalData(ctx, user.ID); err != nil {
		t.Fatalf("wipe: %v", err)
	}

	var streaks int64
	if err := db.Model(&domain.Streak{}).Where("user_id = ?", user.ID).Count(&streaks).Error; err != nil {
		t.Fatalf("streak count: %v", err)
	}
	if streaks != 0 {
		t.Fatalf("streak leftover %d", streaks)
	}
	var usages int64
	if err := db.Model(&domain.UserUsage{}).Where("user_id = ?", user.ID).Count(&usages).Error; err != nil {
		t.Fatalf("usage count: %v", err)
	}
	if usages != 0 {
		t.Fatalf("usage leftover %d", usages)
	}

	var leftoverUser domain.User
	if err := db.First(&leftoverUser, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("account should remain: %v", err)
	}
}
