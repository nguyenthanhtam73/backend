-- Outbound D0/D1 email receipts (≤1 D0 and ≤1 D1 per user) + unsubscribe column.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_unsubscribed_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS email_send_receipts (
    user_id    UUID        NOT NULL,
    kind       VARCHAR(8)  NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, kind)
);

CREATE INDEX IF NOT EXISTS idx_email_send_receipts_kind
    ON email_send_receipts (kind);
