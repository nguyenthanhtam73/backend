-- Monthly feature quota counters (Free plan metering).
-- Schema is also applied via GORM AutoMigrate in repository.AutoMigrate.

CREATE TABLE IF NOT EXISTS user_usages (
    id           UUID PRIMARY KEY,
    user_id      UUID NOT NULL,
    feature_key  VARCHAR(64) NOT NULL,
    usage_count  INTEGER NOT NULL DEFAULT 0,
    period_key   VARCHAR(7) NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One counter row per user × feature × UTC month (period_key = YYYY-MM).
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_usages_user_feature_period
    ON user_usages (user_id, feature_key, period_key);

CREATE INDEX IF NOT EXISTS idx_user_usages_period_end
    ON user_usages (period_end);

CREATE INDEX IF NOT EXISTS idx_user_usages_user_id
    ON user_usages (user_id);
