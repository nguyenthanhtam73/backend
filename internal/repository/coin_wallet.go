package repository

import (
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func creditCoins(tx *gorm.DB, userID uuid.UUID, amount int64, key, reason string, now time.Time) (int64, error) {
	if tx == nil || userID == uuid.Nil {
		return 0, fmt.Errorf("coin credit requires a user")
	}
	if amount <= 0 || key == "" {
		return 0, fmt.Errorf("coin credit requires a positive amount")
	}
	row := domain.UserCoinLedger{
		ID:        key,
		UserID:    userID,
		Amount:    amount,
		Reason:    reason,
		Bucket:    domain.CoinBucketEarned,
		CreatedAt: now.UTC(),
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected > 0 {
		wallet := domain.UserCoinWallet{
			UserID: userID, Balance: amount, EarnedBalance: amount, UpdatedAt: now.UTC(),
		}
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"balance":        gorm.Expr("user_coin_wallets.balance + ?", amount),
				"earned_balance": gorm.Expr("user_coin_wallets.earned_balance + ?", amount),
				"updated_at":     now.UTC(),
			}),
		}).Create(&wallet).Error
		if err != nil {
			return 0, err
		}
	}
	return coinBalance(tx, userID)
}

// debitCoins spends earned xu only. Gift boxes, mystery pieces, and random
// draws must use this so a later paid balance cannot reach them.
func debitCoins(tx *gorm.DB, userID uuid.UUID, amount int64, key, reason string, now time.Time) (int64, bool, error) {
	return spendCoins(tx, userID, amount, key, reason, false, now)
}

// spendCoins is the only shop debit. allowPaid is true for a fixed-price
// regular shop item, including a regular seed piece. Paid xu is not credited
// anywhere yet. When a top-up exists, allowPaid false still refuses it.
func spendCoins(tx *gorm.DB, userID uuid.UUID, amount int64, key, reason string, allowPaid bool, now time.Time) (int64, bool, error) {
	if tx == nil || userID == uuid.Nil {
		return 0, false, fmt.Errorf("coin debit requires a user")
	}
	if amount <= 0 || key == "" {
		return 0, false, fmt.Errorf("coin debit requires a positive amount")
	}
	var existing domain.UserCoinLedger
	err := tx.Where("id = ?", key).Take(&existing).Error
	if err == nil {
		bal, err := coinBalance(tx, userID)
		return bal, false, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}
	var wallet domain.UserCoinWallet
	err = tx.Where("user_id = ?", userID).Take(&wallet).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, ErrNotEnoughCoins
	}
	if err != nil {
		return 0, false, err
	}
	if wallet.EarnedBalance == 0 && wallet.PaidBalance == 0 && wallet.Balance > 0 {
		healed := tx.Model(&domain.UserCoinWallet{}).
			Where("user_id = ? AND earned_balance = 0 AND paid_balance = 0", userID).
			Update("earned_balance", wallet.Balance)
		if healed.Error != nil {
			return 0, false, healed.Error
		}
		wallet.EarnedBalance = wallet.Balance
	}
	earnedUse := wallet.EarnedBalance
	if earnedUse > amount {
		earnedUse = amount
	}
	paidUse := amount - earnedUse
	if paidUse > 0 && (!allowPaid || wallet.PaidBalance < paidUse) {
		return wallet.Balance, false, ErrNotEnoughCoins
	}
	if earnedUse > 0 {
		row := domain.UserCoinLedger{
			ID: key, UserID: userID, Amount: -earnedUse, Reason: reason,
			Bucket: domain.CoinBucketEarned, CreatedAt: now.UTC(),
		}
		if err := tx.Create(&row).Error; err != nil {
			return 0, false, err
		}
	}
	if paidUse > 0 {
		id := key + ":paid"
		if earnedUse == 0 {
			id = key
		}
		row := domain.UserCoinLedger{
			ID: id, UserID: userID, Amount: -paidUse, Reason: reason,
			Bucket: domain.CoinBucketPaid, CreatedAt: now.UTC(),
		}
		if err := tx.Create(&row).Error; err != nil {
			return 0, false, err
		}
	}
	updated := tx.Model(&domain.UserCoinWallet{}).
		Where("user_id = ? AND balance >= ? AND earned_balance >= ? AND paid_balance >= ?", userID, amount, earnedUse, paidUse).
		Updates(map[string]any{
			"balance":        gorm.Expr("balance - ?", amount),
			"earned_balance": gorm.Expr("earned_balance - ?", earnedUse),
			"paid_balance":   gorm.Expr("paid_balance - ?", paidUse),
			"updated_at":     now.UTC(),
		})
	if updated.Error != nil {
		return 0, false, updated.Error
	}
	if updated.RowsAffected == 0 {
		return 0, false, ErrNotEnoughCoins
	}
	bal, err := coinBalance(tx, userID)
	return bal, true, err
}

// backfillCoinBuckets marks every existing balance as earned. Paid stays 0
// until a later top-up. Running it again does not touch a wallet that already
// has a bucket, including one that later holds paid xu.
func backfillCoinBuckets(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&domain.UserCoinWallet{}) {
		return nil
	}
	if db.Migrator().HasTable(&domain.UserCoinLedger{}) {
		err := db.Exec(`UPDATE user_coin_ledger SET bucket = ? WHERE bucket IS NULL OR bucket = ''`, domain.CoinBucketEarned).Error
		if err != nil {
			return err
		}
	}
	return db.Exec(`UPDATE user_coin_wallets SET earned_balance = balance, paid_balance = 0 WHERE earned_balance = 0 AND paid_balance = 0 AND balance != 0`).Error
}

// applyCoinWalletFloors adds the named >= 0 checks from migration 046.
// Postgres boot does not run the SQL file, so AutoMigrate applies the same
// statement. A wallet that is already negative stops the boot instead of
// being rewritten. SQLite tests have no check here.
func applyCoinWalletFloors(db *gorm.DB) error {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return nil
	}
	if !db.Migrator().HasTable(&domain.UserCoinWallet{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&domain.UserCoinWallet{}, "earned_balance") || !db.Migrator().HasColumn(&domain.UserCoinWallet{}, "paid_balance") {
		return nil
	}
	return db.Exec(coinWalletNonnegSQL).Error
}

// coinWalletNonnegSQL is migrations/046_coin_wallet_nonneg.up.sql.
// TestCoinWalletNonnegMigrationMatchesBoot keeps the two copies the same.
const coinWalletNonnegSQL = `-- Floors on the xu wallet.
-- migrations/040_mascot_room_shared.up.sql already declares
--   balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0)
-- on databases that created user_coin_wallets from that file. That check is
-- unnamed in the file; Postgres stores it as user_coin_wallets_balance_check.
-- 045 added earned_balance and paid_balance with no floor.
--
-- Existing rows should already be >= 0. Credits only add, debits require
-- balance >= amount, and 045 copies balance into earned_balance with
-- paid_balance 0. This migration does not rewrite rows. If any wallet is
-- already negative, it stops and leaves the table unchanged.
--
-- Re-running is safe. A column that already has a >= 0 check is skipped,
-- including the balance check from 040.

DO $$
DECLARE
    bad bigint;
    def text;
    has_balance boolean := false;
    has_earned boolean := false;
    has_paid boolean := false;
    rec record;
BEGIN
    SELECT COUNT(*) INTO bad
    FROM user_coin_wallets
    WHERE balance < 0 OR earned_balance < 0 OR paid_balance < 0;
    IF bad > 0 THEN
        RAISE EXCEPTION 'user_coin_wallets has % row(s) below zero; not rewriting them', bad;
    END IF;

    FOR rec IN
        SELECT pg_get_constraintdef(oid) AS def
        FROM pg_constraint
        WHERE conrelid = 'user_coin_wallets'::regclass
          AND contype = 'c'
    LOOP
        def := regexp_replace(lower(rec.def), '[[:space:]]', '', 'g');
        IF def LIKE '%earned_balance>=0%' THEN
            has_earned := true;
        ELSIF def LIKE '%paid_balance>=0%' THEN
            has_paid := true;
        ELSIF def LIKE '%balance>=0%' THEN
            has_balance := true;
        END IF;
    END LOOP;

    IF NOT has_balance THEN
        BEGIN
            ALTER TABLE user_coin_wallets
                ADD CONSTRAINT user_coin_wallets_balance_nonneg CHECK (balance >= 0);
        EXCEPTION
            WHEN duplicate_object THEN NULL;
        END;
    END IF;
    IF NOT has_earned THEN
        BEGIN
            ALTER TABLE user_coin_wallets
                ADD CONSTRAINT user_coin_wallets_earned_balance_nonneg CHECK (earned_balance >= 0);
        EXCEPTION
            WHEN duplicate_object THEN NULL;
        END;
    END IF;
    IF NOT has_paid THEN
        BEGIN
            ALTER TABLE user_coin_wallets
                ADD CONSTRAINT user_coin_wallets_paid_balance_nonneg CHECK (paid_balance >= 0);
        EXCEPTION
            WHEN duplicate_object THEN NULL;
        END;
    END IF;
END $$;
`

func coinBalance(tx *gorm.DB, userID uuid.UUID) (int64, error) {
	var row domain.UserCoinWallet
	err := tx.Where("user_id = ?", userID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return row.Balance, nil
}
