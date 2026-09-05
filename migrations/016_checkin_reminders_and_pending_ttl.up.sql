-- D0/D1 first-check-in reminder snapshots (polled by GET /me/check-in-reminder).
-- Schema is also applied via GORM AutoMigrate in repository.AutoMigrate.

CREATE TABLE IF NOT EXISTS checkin_reminder_flags (
    user_id            UUID PRIMARY KEY,
    kind               VARCHAR(8) NOT NULL DEFAULT 'none',
    due                BOOLEAN NOT NULL DEFAULT FALSE,
    signup_date        DATE NOT NULL,
    checked_in_today   BOOLEAN NOT NULL DEFAULT FALSE,
    days_since_signup  INTEGER NOT NULL DEFAULT 0,
    computed_on        DATE NOT NULL,
    computed_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_checkin_reminder_flags_due
    ON checkin_reminder_flags (due);

CREATE INDEX IF NOT EXISTS idx_checkin_reminder_flags_kind
    ON checkin_reminder_flags (kind);

CREATE INDEX IF NOT EXISTS idx_checkin_reminder_flags_computed_on
    ON checkin_reminder_flags (computed_on);

CREATE INDEX IF NOT EXISTS idx_checkin_reminder_flags_deleted_at
    ON checkin_reminder_flags (deleted_at);

-- Speed leftover pending-order expiry (status + created_at). Existing
-- idx_payment_orders_status remains; this composite helps the TTL sweep.
CREATE INDEX IF NOT EXISTS idx_payment_orders_status_created_at
    ON payment_orders (status, created_at);
