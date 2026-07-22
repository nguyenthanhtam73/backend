DROP TABLE IF EXISTS subscriptions;

DROP INDEX IF EXISTS idx_users_canceled_at;
DROP INDEX IF EXISTS idx_users_trial_ends_at;
DROP INDEX IF EXISTS idx_users_subscription_status;

ALTER TABLE users
    DROP COLUMN IF EXISTS subscription_status,
    DROP COLUMN IF EXISTS canceled_at,
    DROP COLUMN IF EXISTS trial_ends_at;
