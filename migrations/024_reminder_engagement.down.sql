DROP TABLE IF EXISTS push_click_events;

ALTER TABLE users
    DROP COLUMN IF EXISTS push_opt_in_reshow_used_at,
    DROP COLUMN IF EXISTS push_opt_in_skipped_at;

DROP TABLE IF EXISTS email_engagement_events;

DROP INDEX IF EXISTS idx_email_send_receipts_resend_email_id;

ALTER TABLE email_send_receipts
    DROP COLUMN IF EXISTS resend_email_id;
