package repository

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestCoinWalletNonnegMigrationMatchesBoot(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/046_coin_wallet_nonneg.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != strings.TrimSpace(coinWalletNonnegSQL) {
		t.Fatal("migration 046 and the boot SQL differ")
	}
	down, err := os.ReadFile("../../migrations/046_coin_wallet_nonneg.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"user_coin_wallets_balance_nonneg",
		"user_coin_wallets_earned_balance_nonneg",
		"user_coin_wallets_paid_balance_nonneg",
	} {
		if !strings.Contains(string(down), "DROP CONSTRAINT IF EXISTS "+name) {
			t.Fatalf("down migration does not drop %s", name)
		}
	}
}

func TestCoinWalletRejectsNegativeUpdate(t *testing.T) {
	db := openAccountDeletePostgres(t)
	if err := db.Exec(`
		CREATE TABLE users (id uuid PRIMARY KEY);
		CREATE TABLE user_coin_wallets (
			user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
			balance bigint NOT NULL DEFAULT 0,
			earned_balance bigint NOT NULL DEFAULT 0,
			paid_balance bigint NOT NULL DEFAULT 0,
			updated_at timestamptz NOT NULL DEFAULT now()
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := db.Exec(`INSERT INTO users (id) VALUES (?)`, id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_coin_wallets (user_id, balance, earned_balance, paid_balance) VALUES (?, 5, 4, 1)`, id).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../migrations/046_coin_wallet_nonneg.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(raw)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(raw)).Error; err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, col := range []string{"balance", "earned_balance", "paid_balance"} {
		err := db.Exec(fmt.Sprintf(`UPDATE user_coin_wallets SET %s = -1 WHERE user_id = ?`, col), id).Error
		if err == nil {
			t.Fatalf("direct update of %s to -1 was accepted", col)
		}
	}
	var balance int64
	if err := db.Raw(`SELECT balance FROM user_coin_wallets WHERE user_id = ?`, id).Scan(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance != 5 {
		t.Fatalf("balance changed to %d", balance)
	}
	names := walletCheckNames(t, db)
	for _, name := range []string{
		"user_coin_wallets_balance_nonneg",
		"user_coin_wallets_earned_balance_nonneg",
		"user_coin_wallets_paid_balance_nonneg",
	} {
		if _, ok := names[name]; !ok {
			t.Fatalf("missing %s in %v", name, names)
		}
	}
}

func TestCoinWalletFloorSkipsExistingBalanceCheck(t *testing.T) {
	db := openAccountDeletePostgres(t)
	if err := db.Exec(`
		CREATE TABLE users (id uuid PRIMARY KEY);
		CREATE TABLE user_coin_wallets (
			user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
			balance bigint NOT NULL DEFAULT 0 CHECK (balance >= 0),
			earned_balance bigint NOT NULL DEFAULT 0,
			paid_balance bigint NOT NULL DEFAULT 0,
			updated_at timestamptz NOT NULL DEFAULT now()
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../migrations/046_coin_wallet_nonneg.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(raw)).Error; err != nil {
		t.Fatal(err)
	}
	names := walletCheckNames(t, db)
	if _, ok := names["user_coin_wallets_balance_check"]; !ok {
		t.Fatalf("040 check missing: %v", names)
	}
	if _, ok := names["user_coin_wallets_balance_nonneg"]; ok {
		t.Fatalf("added a second balance check: %v", names)
	}
	if _, ok := names["user_coin_wallets_earned_balance_nonneg"]; !ok {
		t.Fatalf("earned check missing: %v", names)
	}
	if _, ok := names["user_coin_wallets_paid_balance_nonneg"]; !ok {
		t.Fatalf("paid check missing: %v", names)
	}
	id := uuid.New()
	if err := db.Exec(`INSERT INTO users (id) VALUES (?)`, id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_coin_wallets (user_id, balance, earned_balance, paid_balance) VALUES (?, 2, 2, 0)`, id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE user_coin_wallets SET balance = -1 WHERE user_id = ?`, id).Error; err == nil {
		t.Fatal("existing balance check allowed -1")
	}
}

func TestCoinWalletFloorStopsOnNegativeRow(t *testing.T) {
	db := openAccountDeletePostgres(t)
	if err := db.Exec(`
		CREATE TABLE users (id uuid PRIMARY KEY);
		CREATE TABLE user_coin_wallets (
			user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
			balance bigint NOT NULL DEFAULT 0,
			earned_balance bigint NOT NULL DEFAULT 0,
			paid_balance bigint NOT NULL DEFAULT 0,
			updated_at timestamptz NOT NULL DEFAULT now()
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := db.Exec(`INSERT INTO users (id) VALUES (?)`, id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_coin_wallets (user_id, balance, earned_balance, paid_balance) VALUES (?, -3, 0, 0)`, id).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../migrations/046_coin_wallet_nonneg.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	err = db.Exec(string(raw)).Error
	if err == nil || !strings.Contains(err.Error(), "below zero") {
		t.Fatalf("negative row was rewritten or ignored: %v", err)
	}
	if len(walletCheckNames(t, db)) != 0 {
		t.Fatalf("checks were added despite a negative row: %v", walletCheckNames(t, db))
	}
}

func walletCheckNames(t *testing.T, db *gorm.DB) map[string]struct{} {
	t.Helper()
	var names []string
	err := db.Raw(`
		SELECT conname FROM pg_constraint
		WHERE conrelid = 'user_coin_wallets'::regclass AND contype = 'c'
	`).Scan(&names).Error
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out
}
