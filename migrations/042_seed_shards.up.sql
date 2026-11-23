-- Seed shards for the shared room tree.
-- Rates and the shard price are rows, not code.
-- tree_types.plantable is separate from enabled: a rare tree can be planted
-- from a mystery seed and stay out of the public catalog until that user
-- has revealed it.
-- user_coin_ledger amounts may be negative for a shard purchase. The wallet
-- balance stays >= 0.
-- Bloom grants are unique per (room, user). The ledger source is one of
-- bloom, trade_in, trade_out, buy, merge, plant.

ALTER TABLE tree_types
    ADD COLUMN IF NOT EXISTS plantable BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE tree_types SET plantable = TRUE
WHERE key IN ('leaf', 'cherry', 'aloe', 'sunflower', 'dandelion', 'quynh', 'sen_hong');

ALTER TABLE user_coin_ledger DROP CONSTRAINT IF EXISTS user_coin_ledger_amount_check;
ALTER TABLE user_coin_ledger ADD CONSTRAINT user_coin_ledger_amount_check CHECK (amount <> 0);

CREATE TABLE IF NOT EXISTS seed_bag_config (
    key VARCHAR(64) PRIMARY KEY,
    int_value INTEGER NOT NULL
);

INSERT INTO seed_bag_config (key, int_value) VALUES
    ('mystery_shard_bps', 1000),
    ('shard_buy_price', 5000),
    ('merge_count', 5),
    ('trade_in_count', 3)
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS user_seed_balances (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,
    type_key VARCHAR(64) NOT NULL,
    count INTEGER NOT NULL CHECK (count >= 0),
    PRIMARY KEY (user_id, kind, type_key)
);

CREATE TABLE IF NOT EXISTS user_seed_ledger (
    id VARCHAR(80) PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,
    type_key VARCHAR(64) NOT NULL,
    delta INTEGER NOT NULL CHECK (delta <> 0),
    source VARCHAR(16) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS user_seed_ledger_user_idx ON user_seed_ledger (user_id);

CREATE TABLE IF NOT EXISTS mascot_room_bloom_shards (
    room_code VARCHAR(6) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type_key VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_code, user_id)
);

CREATE TABLE IF NOT EXISTS user_tree_reveals (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type_key VARCHAR(64) NOT NULL REFERENCES tree_types(key),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, type_key)
);
