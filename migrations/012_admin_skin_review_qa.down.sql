ALTER TABLE admin_skin_reviews
    DROP COLUMN IF EXISTS user_question,
    DROP COLUMN IF EXISTS answer;
