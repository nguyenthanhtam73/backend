-- Operator corrections + photo context for Admin Skin Review.
--
-- skin_context: touch / pain / duration answers that separate look-alike groups a
-- photo cannot (milia vs closed comedones vs skin tags).
-- analysis_original + analysis_corrected_at: when an operator fixes the AI's group, the
-- model's first answer is kept so the pair becomes labeled data for accuracy eval.
-- Also applied via AutoMigrate on domain.AdminSkinReview.
ALTER TABLE admin_skin_reviews
    ADD COLUMN IF NOT EXISTS skin_context TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS analysis_original JSONB,
    ADD COLUMN IF NOT EXISTS analysis_corrected_at TIMESTAMPTZ;

-- Corrected rows are the eval set; keep them cheap to list.
CREATE INDEX IF NOT EXISTS idx_admin_skin_reviews_corrected_at
    ON admin_skin_reviews (analysis_corrected_at)
    WHERE analysis_corrected_at IS NOT NULL;
