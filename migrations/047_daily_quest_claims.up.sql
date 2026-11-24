-- Private-room daily quest chest (morning / evening routine).
-- Additive only. Existing routines, wallets, and ledger rows stay.
--
-- Production does not run this file. cmd/api calls repository.AutoMigrate,
-- and Railway's deploy does not execute migrations/ (see the note on
-- migrations/024_account_deletion_anonymize.up.sql). domain.DailyQuestClaim
-- carries the same primary key, foreign key, and checks, with the same
-- constraint names. Apply this file only when AutoMigrate is not the source
-- of truth.
--
-- One row per (user, care day, session). The care day is the Vietnam
-- calendar day with a 04:00 rollover (the same day routine ticks use).
-- A second insert for that key does nothing, so a double tap or two
-- devices cannot credit the wallet twice. The session credit and the
-- both-sessions bonus are user_coin_ledger rows
-- (daily_quest_morning / daily_quest_evening / daily_quest_bonus).
-- Deleting the account removes the claim rows.

CREATE TABLE IF NOT EXISTS daily_quest_claims (
    user_id UUID NOT NULL,
    day VARCHAR(10) NOT NULL,
    session VARCHAR(16) NOT NULL,
    coins INTEGER NOT NULL,
    bonus INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, day, session),
    CONSTRAINT daily_quest_claims_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT daily_quest_claims_coins_check CHECK (coins >= 0),
    CONSTRAINT daily_quest_claims_bonus_check CHECK (bonus >= 0),
    CONSTRAINT daily_quest_claims_session_check CHECK (session IN ('morning', 'evening'))
);
