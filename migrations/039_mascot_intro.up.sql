-- Bé Da intro: the account remembers the chosen gender and that the
-- introduction page was finished. Existing rows stay female and unseen.
-- ADD COLUMN ... DEFAULT backfills gender. intro_seen_at stays null until
-- POST /api/v1/me/mascot/intro. Boot schema also comes from GORM AutoMigrate.
-- Apply this file when AutoMigrate is not the source of truth.

ALTER TABLE user_mascot
    ADD COLUMN IF NOT EXISTS gender VARCHAR(16) NOT NULL DEFAULT 'female';

ALTER TABLE user_mascot
    ADD COLUMN IF NOT EXISTS intro_seen_at TIMESTAMPTZ;
