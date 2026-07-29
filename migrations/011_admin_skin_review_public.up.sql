-- Public share fields for Admin Skin Review (Facebook share links).
-- Also applied via AutoMigrate on domain.AdminSkinReview.
ALTER TABLE admin_skin_reviews
    ADD COLUMN IF NOT EXISTS public_image_paths JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS is_public BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS public_slug VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ NULL;

-- Unique slug among non-empty values (drafts keep '').
CREATE UNIQUE INDEX IF NOT EXISTS idx_admin_skin_reviews_public_slug
    ON admin_skin_reviews (public_slug)
    WHERE public_slug <> '';

CREATE INDEX IF NOT EXISTS idx_admin_skin_reviews_is_public
    ON admin_skin_reviews (is_public)
    WHERE is_public = TRUE;
