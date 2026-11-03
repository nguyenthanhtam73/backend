-- Bé Da glow level on the existing mascot row.
-- glow_level is the highest level reached and is only written upward.
-- glow_level_seen is the level the client last acknowledged
-- (POST /api/v1/me/mascot/glow/seen). Default 1 is the starting level,
-- so a new mascot does not celebrate.
-- Readers return max(glow_level, the level from distinct Asia/Ho_Chi_Minh
-- check-in days). Boot schema also comes from GORM AutoMigrate. Apply this
-- file when AutoMigrate is not the source of truth.
-- ADD COLUMN ... DEFAULT backfills existing rows.

ALTER TABLE user_mascot
    ADD COLUMN IF NOT EXISTS glow_level INTEGER NOT NULL DEFAULT 1;

ALTER TABLE user_mascot
    ADD COLUMN IF NOT EXISTS glow_level_seen INTEGER NOT NULL DEFAULT 1;
