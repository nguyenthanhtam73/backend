-- Client-reported paywall / upsell / upgrade impressions for founder funnel stats.
-- Schema is also applied via GORM AutoMigrate in repository.AutoMigrate.

CREATE TABLE IF NOT EXISTS paywall_views (
    id               UUID PRIMARY KEY,
    user_id          UUID,
    surface          VARCHAR(32) NOT NULL,
    feature          VARCHAR(64) NOT NULL DEFAULT 'generic',
    recommended_plan VARCHAR(32) NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_paywall_views_created_at
    ON paywall_views (created_at);

CREATE INDEX IF NOT EXISTS idx_paywall_views_user_id
    ON paywall_views (user_id);

CREATE INDEX IF NOT EXISTS idx_paywall_views_surface
    ON paywall_views (surface);
