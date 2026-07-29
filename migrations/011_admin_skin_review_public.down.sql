DROP INDEX IF EXISTS idx_admin_skin_reviews_is_public;
DROP INDEX IF EXISTS idx_admin_skin_reviews_public_slug;

ALTER TABLE admin_skin_reviews
    DROP COLUMN IF EXISTS published_at,
    DROP COLUMN IF EXISTS public_slug,
    DROP COLUMN IF EXISTS is_public,
    DROP COLUMN IF EXISTS public_image_paths;
