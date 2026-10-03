ALTER TABLE users
    DROP COLUMN IF EXISTS email_reminder_suppressed_at,
    DROP COLUMN IF EXISTS email_reminder_hash,
    DROP COLUMN IF EXISTS email_reminder_fail_count;
