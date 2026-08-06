-- Public Q&A fields for Admin Skin Review (FB question + admin/AI answer).
-- Also applied via AutoMigrate on domain.AdminSkinReview.
ALTER TABLE admin_skin_reviews
    ADD COLUMN IF NOT EXISTS user_question TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS answer TEXT NOT NULL DEFAULT '';
