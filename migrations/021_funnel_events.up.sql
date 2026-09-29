-- First-party check-in funnel events (page view through submit).
-- Numbered 021 so it lands after 020_email_reminder_suppression.
-- user_id is NULL for guests. Do not add IP or email columns.
-- Boot schema comes from GORM AutoMigrate in repository.AutoMigrate, which
-- does not read this directory. Apply this file by hand on Postgres when
-- that AutoMigrate is not the source of truth.

CREATE TABLE IF NOT EXISTS funnel_events (
    id         UUID PRIMARY KEY,
    user_id    UUID,
    session_id VARCHAR(64)  NOT NULL,
    event      VARCHAR(40)  NOT NULL,
    path       VARCHAR(200) NOT NULL DEFAULT '',
    props      JSONB        NOT NULL DEFAULT '{}'::jsonb,
    client_ts  TIMESTAMPTZ  NOT NULL,
    server_ts  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    user_agent VARCHAR(256) NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_funnel_events_user_server
    ON funnel_events (user_id, server_ts);

CREATE INDEX IF NOT EXISTS idx_funnel_events_event_server
    ON funnel_events (event, server_ts);
