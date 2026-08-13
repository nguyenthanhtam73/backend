DROP INDEX IF EXISTS idx_admin_skin_reviews_corrected_at;

ALTER TABLE admin_skin_reviews
    DROP COLUMN IF EXISTS analysis_corrected_at,
    DROP COLUMN IF EXISTS analysis_original,
    DROP COLUMN IF EXISTS skin_context;
