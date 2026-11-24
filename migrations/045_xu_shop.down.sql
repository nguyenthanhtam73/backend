DROP TABLE IF EXISTS user_shop_unlocks;
DROP TABLE IF EXISTS user_shop_orders;

ALTER TABLE user_coin_ledger DROP COLUMN IF EXISTS bucket;
ALTER TABLE user_coin_wallets DROP COLUMN IF EXISTS paid_balance;
ALTER TABLE user_coin_wallets DROP COLUMN IF EXISTS earned_balance;
