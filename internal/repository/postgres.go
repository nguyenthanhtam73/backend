// Package repository will hold persistence implementations (GORM, caches).
package repository

import (
	"fmt"
	"log/slog"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// NewPostgres opens a GORM connection using the configured URL.
func NewPostgres(cfg *config.Config) (*gorm.DB, error) {
	if cfg.Database.URL == "" {
		return nil, fmt.Errorf("database url is empty")
	}

	db, err := gorm.Open(postgres.Open(cfg.Database.URL), &gorm.Config{
		Logger: newGormLogger(cfg.Env),
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	return db, nil
}

// AutoMigrate runs schema migrations for core domain models (dev/small deploys).
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.RefreshSession{},
		&domain.OnboardingPreviewJob{},
		&domain.RoutineSuggestJob{},
		&domain.SkinProfile{},
		&domain.SkinCheck{},
		&domain.SkinAnalysis{},
		&domain.RoutineEntry{},
		&domain.SkincareProduct{},
		&domain.AffiliateClick{},
		&domain.PaywallView{},
		&domain.FunnelEvent{},
		&domain.ProgressLog{},
		&domain.AIUserFeedback{},
		&domain.Feedback{},
		&domain.AdminSkinReview{},
		&domain.BetaSignup{},
		&domain.UsageEvent{},
		&domain.UserUsage{},
		&domain.PlanChangeLog{},
		&domain.PaymentOrder{},
		&domain.PaymentOpsEvent{},
		&domain.CheckInReminderFlag{},
		&domain.EmailSendReceipt{},
		&domain.EmailEngagementEvent{},
		&domain.PushClickEvent{},
		&domain.Subscription{},
		&domain.Streak{},
		&domain.PushSubscription{},
		&domain.PushJobLock{},
		&domain.PushSendReceipt{},
		&domain.AccountDeleteOrphanKey{},
	); err != nil {
		return err
	}
	return applyCaptureReminderClaims(db)
}

// applyCaptureReminderClaims creates capture_reminder_claims (migration 029).
// An INFO line is written only when this boot creates the table.
func applyCaptureReminderClaims(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	existed := db.Migrator().HasTable(&domain.CaptureReminderClaim{})
	if err := db.AutoMigrate(&domain.CaptureReminderClaim{}); err != nil {
		return err
	}
	if !existed {
		slog.Info("capture_reminder_claims: migration 029 applied")
	}
	return nil
}
