-- Lease/TTL on job locks (crash recovery) + per-user send receipts (safe retry).

ALTER TABLE push_job_locks
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Backfill: treat existing same-day claims as already expired so a deploy can
-- reclaim safely if a prior pod died mid-run.
UPDATE push_job_locks
SET expires_at = NOW() - INTERVAL '1 minute'
WHERE last_run_date <> '' AND expires_at = claimed_at;

CREATE TABLE IF NOT EXISTS push_send_receipts (
    user_id           UUID        NOT NULL,
    notification_type VARCHAR(64) NOT NULL,
    run_date          VARCHAR(10) NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, notification_type, run_date)
);

CREATE INDEX IF NOT EXISTS idx_push_send_receipts_run
    ON push_send_receipts (notification_type, run_date);
