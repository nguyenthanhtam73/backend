-- Admin plan grant/revoke audit trail (internal testing).
-- Schema is also applied via GORM AutoMigrate in repository.AutoMigrate.

CREATE TABLE IF NOT EXISTS plan_change_logs (
    id            UUID PRIMARY KEY,
    user_id       UUID NOT NULL,
    actor_user_id UUID NOT NULL,
    actor_email   VARCHAR(255) NOT NULL,
    from_plan     VARCHAR(16) NOT NULL,
    to_plan       VARCHAR(16) NOT NULL,
    reason        TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plan_change_logs_user_id
    ON plan_change_logs (user_id);

CREATE INDEX IF NOT EXISTS idx_plan_change_logs_actor_user_id
    ON plan_change_logs (actor_user_id);

CREATE INDEX IF NOT EXISTS idx_plan_change_logs_created_at
    ON plan_change_logs (created_at DESC);
