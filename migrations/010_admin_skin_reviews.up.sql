-- Admin Skin Review: observations-only AI sessions (no Free quota; admin allow-list).
-- Also applied via AutoMigrate on domain.AdminSkinReview.
CREATE TABLE IF NOT EXISTS admin_skin_reviews (
    id UUID PRIMARY KEY,
    admin_user_id UUID NOT NULL,
    title VARCHAR(200) NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    image_paths JSONB NOT NULL DEFAULT '[]',
    analysis JSONB NOT NULL,
    locale VARCHAR(8) NOT NULL DEFAULT 'vi',
    model_used VARCHAR(120) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_admin_skin_reviews_admin_user_id
    ON admin_skin_reviews (admin_user_id);

CREATE INDEX IF NOT EXISTS idx_admin_skin_reviews_status
    ON admin_skin_reviews (status);

CREATE INDEX IF NOT EXISTS idx_admin_skin_reviews_created_at
    ON admin_skin_reviews (created_at DESC);
