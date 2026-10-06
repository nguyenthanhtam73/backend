-- Per-user reminder schedule. NULL reminder_enabled means the user never
-- chose, so every outbound capture/check-in reminder keeps today's behavior.
-- false turns those jobs off for that user (20:00 push, streak-at-risk,
-- evening D1/Day-3 email, hourly D0 email, hourly D0/D1 push).
-- reminder_time is a local HH:MM and is stored only; send time stays on
-- the shared clocks. Transactional mail does not read this column.
-- Boot schema also comes from GORM AutoMigrate. Apply this file on Postgres
-- when AutoMigrate is not the source of truth.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS reminder_enabled BOOLEAN,
    ADD COLUMN IF NOT EXISTS reminder_time VARCHAR(5),
    ADD COLUMN IF NOT EXISTS reminder_timezone VARCHAR(64);
