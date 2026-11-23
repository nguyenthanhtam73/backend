DROP TABLE IF EXISTS user_tree_reveals;
DROP TABLE IF EXISTS mascot_room_bloom_shards;
DROP TABLE IF EXISTS user_seed_ledger;
DROP TABLE IF EXISTS user_seed_balances;
DROP TABLE IF EXISTS seed_bag_config;

ALTER TABLE tree_types DROP COLUMN IF EXISTS mystery_pool;
ALTER TABLE tree_types DROP COLUMN IF EXISTS plantable;

-- Restoring CHECK (amount > 0) fails once a shard_buy row stores a negative
-- amount. Delete those user_coin_ledger rows before running this down, or
-- leave the check dropped.
ALTER TABLE user_coin_ledger DROP CONSTRAINT IF EXISTS user_coin_ledger_amount_check;
ALTER TABLE user_coin_ledger ADD CONSTRAINT user_coin_ledger_amount_check CHECK (amount > 0);
