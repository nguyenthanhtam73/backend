-- Coach voice for one skin check.
-- android = polite mình/bạn (Play app). Existing rows stay on the web voice.
-- API boot also adds this column via GORM AutoMigrate, same approach as
-- refresh_sessions.client_kind. Apply this file when AutoMigrate is not the
-- source of truth. ADD COLUMN ... DEFAULT backfills existing rows.

ALTER TABLE skin_checks
    ADD COLUMN IF NOT EXISTS client_kind VARCHAR(16) NOT NULL DEFAULT 'web';
