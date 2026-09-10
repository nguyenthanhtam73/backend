-- One live routine row per user per calendar day.
-- AutoMigrate only adds a non-unique index; apply this on Railway Postgres.
CREATE UNIQUE INDEX IF NOT EXISTS idx_routine_entries_user_date_live
    ON routine_entries (user_id, routine_date)
    WHERE deleted_at IS NULL;
