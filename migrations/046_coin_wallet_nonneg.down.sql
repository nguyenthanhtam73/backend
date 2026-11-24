-- Drops only the named floors added by 046. The balance check created with
-- the table in 040 (user_coin_wallets_balance_check) stays.

ALTER TABLE user_coin_wallets DROP CONSTRAINT IF EXISTS user_coin_wallets_paid_balance_nonneg;
ALTER TABLE user_coin_wallets DROP CONSTRAINT IF EXISTS user_coin_wallets_earned_balance_nonneg;
ALTER TABLE user_coin_wallets DROP CONSTRAINT IF EXISTS user_coin_wallets_balance_nonneg;
