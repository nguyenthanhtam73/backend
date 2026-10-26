-- One capture-reminder moment per user per local civil date.
-- scheduled_capture inserts a row before sending so a restart or a second
-- replica cannot send again. Fixed-clock jobs leave users with a saved
-- schedule (reminder_enabled true and reminder_time set) out of their
-- candidate queries. NULL reminder_enabled keeps the shared clocks.
-- Boot schema also comes from GORM AutoMigrate. Apply this file on Postgres
-- when AutoMigrate is not the source of truth.

CREATE TABLE IF NOT EXISTS capture_reminder_claims (
    user_id    UUID        NOT NULL,
    local_date VARCHAR(10) NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, local_date)
);
