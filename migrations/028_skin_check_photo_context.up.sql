-- Close-up zone and touch / duration / pain answers for one skin check.
-- Shape: {"images":[...],"skin_context":{...}}. Null on older rows (no backfill).
-- The column lives on skin_checks, so account deletion removes it with the
-- check-in row. GET /me/export includes the same JSON.
-- Boot schema also comes from GORM AutoMigrate. Apply this file on Postgres
-- when AutoMigrate is not the source of truth.

ALTER TABLE skin_checks
    ADD COLUMN IF NOT EXISTS photo_context JSONB;
