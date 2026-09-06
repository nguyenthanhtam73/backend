DROP TABLE IF EXISTS email_send_receipts;

ALTER TABLE users
    DROP COLUMN IF EXISTS email_unsubscribed_at;
