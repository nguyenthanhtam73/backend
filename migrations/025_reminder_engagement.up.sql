-- D1/D3 reminder emails at 19:30 ICT, Resend open/click events, push clicks,
-- and the one-time push opt-in re-show flag.
-- Boot schema also comes from GORM AutoMigrate. Apply this file on Postgres
-- when AutoMigrate is not the source of truth.

ALTER TABLE email_send_receipts
    ADD COLUMN IF NOT EXISTS resend_email_id VARCHAR(64) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_email_send_receipts_resend_email_id
    ON email_send_receipts (resend_email_id);

CREATE TABLE IF NOT EXISTS email_engagement_events (
    id              UUID PRIMARY KEY,
    resend_event_id VARCHAR(128) NOT NULL,
    resend_email_id VARCHAR(64)  NOT NULL DEFAULT '',
    user_id         UUID,
    kind            VARCHAR(8)   NOT NULL DEFAULT '',
    event_type      VARCHAR(16)  NOT NULL,
    link_url        VARCHAR(2048) NOT NULL DEFAULT '',
    occurred_at     TIMESTAMPTZ  NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_email_engagement_resend_event
    ON email_engagement_events (resend_event_id);

CREATE INDEX IF NOT EXISTS idx_email_engagement_user
    ON email_engagement_events (user_id, occurred_at);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS push_opt_in_skipped_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS push_opt_in_reshow_used_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS push_click_events (
    id                UUID PRIMARY KEY,
    user_id           UUID         NOT NULL,
    notification_kind VARCHAR(64)  NOT NULL DEFAULT '',
    tag               VARCHAR(128) NOT NULL DEFAULT '',
    idempotency_key   VARCHAR(128) NOT NULL,
    clicked_at        TIMESTAMPTZ  NOT NULL,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_push_click_user_idem
    ON push_click_events (user_id, idempotency_key);

CREATE INDEX IF NOT EXISTS idx_push_click_user_time
    ON push_click_events (user_id, clicked_at);
