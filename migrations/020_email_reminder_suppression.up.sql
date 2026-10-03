-- Stop hourly D0/D1 reminder retries once an address is undeliverable.
-- Schema is also applied via GORM AutoMigrate in repository.AutoMigrate.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_reminder_fail_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS email_reminder_hash VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS email_reminder_suppressed_at TIMESTAMPTZ;
