-- Subscription lifecycle: trial, cancel, grace, renew.
-- Current state on users; append-only history in subscriptions.
-- Schema is also applied via GORM AutoMigrate in repository.AutoMigrate.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS trial_ends_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS canceled_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS subscription_status VARCHAR(16) NOT NULL DEFAULT 'none';

CREATE INDEX IF NOT EXISTS idx_users_subscription_status
    ON users (subscription_status);

CREATE INDEX IF NOT EXISTS idx_users_trial_ends_at
    ON users (trial_ends_at)
    WHERE trial_ends_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_canceled_at
    ON users (canceled_at)
    WHERE canceled_at IS NOT NULL;

-- Backfill: paid rows with future/NULL expiry → active; expired paid → past_due
-- (cron will move past_due → free/expired after grace).
UPDATE users
SET subscription_status = CASE
    WHEN plan_tier IN ('premium', 'premium_plus')
         AND (plan_expires_at IS NULL OR plan_expires_at > NOW())
        THEN 'active'
    WHEN plan_tier IN ('premium', 'premium_plus')
         AND plan_expires_at IS NOT NULL
         AND plan_expires_at <= NOW()
        THEN 'past_due'
    ELSE 'none'
END
WHERE subscription_status = 'none'
   OR subscription_status IS NULL
   OR subscription_status = '';

CREATE TABLE IF NOT EXISTS subscriptions (
    id                UUID PRIMARY KEY,
    user_id           UUID NOT NULL,
    plan_tier         VARCHAR(16) NOT NULL,
    billing_interval  VARCHAR(16),
    status            VARCHAR(16) NOT NULL,
    event_type        VARCHAR(32) NOT NULL,
    provider          VARCHAR(16) NOT NULL DEFAULT 'sepay',
    external_ref      VARCHAR(128),
    trial_ends_at     TIMESTAMPTZ,
    period_starts_at  TIMESTAMPTZ,
    period_ends_at    TIMESTAMPTZ,
    canceled_at       TIMESTAMPTZ,
    grace_ends_at     TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id
    ON subscriptions (user_id);

CREATE INDEX IF NOT EXISTS idx_subscriptions_created_at
    ON subscriptions (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_subscriptions_event_type
    ON subscriptions (event_type);

CREATE INDEX IF NOT EXISTS idx_subscriptions_status
    ON subscriptions (status);

CREATE INDEX IF NOT EXISTS idx_subscriptions_external_ref
    ON subscriptions (external_ref)
    WHERE external_ref IS NOT NULL AND external_ref <> '';
