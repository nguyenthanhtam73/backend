DROP TABLE IF EXISTS push_send_receipts;

ALTER TABLE push_job_locks
    DROP COLUMN IF EXISTS expires_at;
