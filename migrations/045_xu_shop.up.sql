-- Xu shop orders, the permanent Ngọc Trai unlock, and earned vs paid xu.
-- Existing balances are earned. Paid stays 0 until a later top-up.
-- AutoMigrate adds the same tables and columns on boot.

ALTER TABLE user_coin_wallets
    ADD COLUMN IF NOT EXISTS earned_balance BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS paid_balance BIGINT NOT NULL DEFAULT 0;

ALTER TABLE user_coin_ledger
    ADD COLUMN IF NOT EXISTS bucket VARCHAR(16) NOT NULL DEFAULT 'earned';

UPDATE user_coin_ledger
SET bucket = 'earned'
WHERE bucket IS NULL OR bucket = '';

UPDATE user_coin_wallets
SET earned_balance = balance,
    paid_balance = 0
WHERE earned_balance = 0
  AND paid_balance = 0
  AND balance <> 0;

CREATE TABLE IF NOT EXISTS user_shop_orders (
    user_id UUID NOT NULL REFERENCES users (id) ON UPDATE CASCADE ON DELETE CASCADE,
    idem_key VARCHAR(80) NOT NULL,
    item_id VARCHAR(64) NOT NULL,
    tree_type VARCHAR(64) NOT NULL DEFAULT '',
    charged INTEGER NOT NULL,
    granted_ids VARCHAR(512) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, idem_key)
);

CREATE TABLE IF NOT EXISTS user_shop_unlocks (
    user_id UUID NOT NULL REFERENCES users (id) ON UPDATE CASCADE ON DELETE CASCADE,
    unlock_key VARCHAR(64) NOT NULL,
    unlocked_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, unlock_key)
);
