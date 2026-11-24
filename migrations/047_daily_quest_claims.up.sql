-- Private-room daily quest chest (morning / evening routine).
-- Additive only. Existing routines, wallets, and ledger rows stay.
-- Boot schema also comes from GORM AutoMigrate of domain.DailyQuestClaim.
-- Apply this file on Postgres when AutoMigrate is not the source of truth.
--
-- One row per (user, care day, session). The care day is the Vietnam
-- calendar day with a 04:00 rollover (the same day routine ticks use).
-- A second insert for that key does nothing, so a double tap or two
-- devices cannot credit the wallet twice. The session credit and the
-- both-sessions bonus are user_coin_ledger rows
-- (daily_quest_morning / daily_quest_evening / daily_quest_bonus).
-- Deleting the account removes the claim rows.

CREATE TABLE IF NOT EXISTS daily_quest_claims (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day VARCHAR(10) NOT NULL,
    session VARCHAR(16) NOT NULL,
    coins INTEGER NOT NULL CHECK (coins >= 0),
    bonus INTEGER NOT NULL DEFAULT 0 CHECK (bonus >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, day, session),
    CONSTRAINT daily_quest_claims_session_check CHECK (session IN ('morning', 'evening'))
);
