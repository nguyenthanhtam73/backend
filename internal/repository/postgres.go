// Package repository will hold persistence implementations (GORM, caches).
package repository

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

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
	if err := prepareMascotRoomEnds(db); err != nil {
		return err
	}
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
		&domain.MascotItem{},
		&domain.UserMascot{},
		&domain.UserMascotItem{},
		&domain.MascotRoom{},
		&domain.MascotRoomMember{},
		&domain.MascotRoomActiveSeat{},
		&domain.MascotRoomQuestClaim{},
		&domain.MascotRoomNudge{},
		&domain.UserCoinWallet{},
		&domain.UserCoinLedger{},
		&domain.TreeType{},
		&domain.MascotRoomTree{},
		&domain.MascotRoomTreeWater{},
		&domain.MascotRoomTreeReward{},
		&domain.MascotRoomMemberLeave{},
		&domain.SeedBagConfig{},
		&domain.UserSeedBalance{},
		&domain.UserSeedLedger{},
		&domain.RoomBloomShard{},
		&domain.UserTreeReveal{},
	); err != nil {
		return err
	}
	if err := SeedMascotCatalog(db); err != nil {
		return err
	}
	if err := SeedTreeCatalog(db); err != nil {
		return err
	}
	if err := SeedSeedBag(db); err != nil {
		return err
	}
	if err := applyRoomTreeFK(db); err != nil {
		return err
	}
	if err := applyShardBuyLedger(db); err != nil {
		return err
	}
	if err := applyRoutineFullDay(db); err != nil {
		return err
	}
	if err := applyCaptureReminderClaims(db); err != nil {
		return err
	}
	if err := applyMascotRoomTerm(db); err != nil {
		return err
	}
	return PrepareOutageApologySchema(db)
}

// applyShardBuyLedger lets a shard purchase store a negative ledger amount.
// The wallet check still refuses a balance below zero.
func applyShardBuyLedger(db *gorm.DB) error {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return nil
	}
	if !db.Migrator().HasTable(&domain.UserCoinLedger{}) {
		return nil
	}
	var def string
	err := db.Raw(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'user_coin_ledger_amount_check'`).Scan(&def).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	switch ledgerAmountCheckAction(def) {
	case "keep":
		return nil
	case "replace":
		if err := db.Exec(`ALTER TABLE user_coin_ledger DROP CONSTRAINT IF EXISTS user_coin_ledger_amount_check`).Error; err != nil {
			return err
		}
	}
	return db.Exec(`ALTER TABLE user_coin_ledger ADD CONSTRAINT user_coin_ledger_amount_check CHECK (amount <> 0)`).Error
}

// ledgerAmountCheckAction reports whether the amount check already allows a debit.
func ledgerAmountCheckAction(def string) string {
	low := strings.ToLower(strings.ReplaceAll(def, " ", ""))
	if strings.Contains(low, "<>0") || strings.Contains(low, "!=0") {
		return "keep"
	}
	if strings.TrimSpace(def) == "" {
		return "add"
	}
	return "replace"
}

// applyRoomTreeFK points mascot_rooms.tree_type at tree_types on Postgres.
// SQLite tests have no foreign key. A blank type is stored as leaf first.
func applyRoomTreeFK(db *gorm.DB) error {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return nil
	}
	if !db.Migrator().HasTable(&domain.MascotRoom{}) || !db.Migrator().HasTable(&domain.TreeType{}) {
		return nil
	}
	if err := db.Exec(`UPDATE mascot_rooms SET tree_type = 'leaf' WHERE tree_type IS NULL OR tree_type = ''`).Error; err != nil {
		return err
	}
	return db.Exec(`
		DO $$ BEGIN
			ALTER TABLE mascot_rooms
				ADD CONSTRAINT mascot_rooms_tree_type_fkey
				FOREIGN KEY (tree_type) REFERENCES tree_types(key);
		EXCEPTION
			WHEN duplicate_object THEN NULL;
		END $$;
	`).Error
}

// PrepareOutageApologySchema adds the starter-generation lease columns and
// widens email_send_receipts.kind so the marker ai_outage_apology_20261106 fits.
// Adding a column and widening a varchar does not rewrite existing rows.
func PrepareOutageApologySchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	if err := db.AutoMigrate(&domain.SkinProfile{}, &domain.SkinAnalysis{}, &domain.EmailSendReceipt{}); err != nil {
		return err
	}
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Exec(`ALTER TABLE email_send_receipts ALTER COLUMN kind TYPE VARCHAR(64)`).Error
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
