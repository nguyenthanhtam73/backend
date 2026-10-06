-- Per-user reminder schedule. NULL reminder_enabled means the user never
-- chose, so the 20:00 push and evening reminder emails keep today's behavior.
-- false turns those jobs off for that user. reminder_time is a local HH:MM
-- and is stored only; send time stays on the shared clocks.
-- Boot schema also comes from GORM AutoMigrate. Apply this file on Postgres
-- when AutoMigrate is not the source of truth.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS reminder_enabled BOOLEAN,
    ADD COLUMN IF NOT EXISTS reminder_time VARCHAR(5),
    ADD COLUMN IF NOT EXISTS reminder_timezone VARCHAR(64);
