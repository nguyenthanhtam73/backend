-- Shared-room quest chests, daily nudges, and the coin wallet they pay.
-- Additive only. Existing rooms, members, routines, and skin checks stay.
-- Boot schema also comes from GORM AutoMigrate. Apply this file on Postgres
-- when AutoMigrate is not the source of truth.
--
-- A quest claim is unique on (room, milestone member-days, user) so a chest
-- pays at most once. A nudge is unique on (room, sender, target, Vietnam
-- date) so one person can nudge another at most once per civil day.
-- Deleting a room or an account removes the rows that pointed at it.
-- The down file drops only these new tables.
--
-- There was no coin balance table before this migration. Chests credit
-- user_coin_wallets and append user_coin_ledger. The ledger id is the
-- idempotency key roomquest:<room>:<member_days>:<user>.

CREATE TABLE IF NOT EXISTS mascot_room_quest_claims (
    room_code VARCHAR(6) NOT NULL REFERENCES mascot_rooms(code) ON DELETE CASCADE,
    member_days INTEGER NOT NULL CHECK (member_days > 0),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    coins INTEGER NOT NULL CHECK (coins >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_code, member_days, user_id)
);

CREATE TABLE IF NOT EXISTS mascot_room_nudges (
    room_code VARCHAR(6) NOT NULL REFERENCES mascot_rooms(code) ON DELETE CASCADE,
    sender_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    vn_date VARCHAR(10) NOT NULL,
    message VARCHAR(240) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_code, sender_user_id, target_user_id, vn_date)
);

CREATE TABLE IF NOT EXISTS user_coin_wallets (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_coin_ledger (
    id VARCHAR(80) PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount BIGINT NOT NULL CHECK (amount > 0),
    reason VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS user_coin_ledger_user_idx ON user_coin_ledger (user_id);
