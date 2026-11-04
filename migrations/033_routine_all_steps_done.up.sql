-- Full-routine day for Bé Da missed days.
-- all_steps_done is true when every stored morning and evening step is ticked
-- (domain.RoutineAllStepsDone). An empty checklist stays false.
-- Writers set it in RoutineEntry.BeforeSave. AutoMigrate backfills existing
-- rows once, then creates idx_routine_entries_full_day_live so
-- LatestFullRoutineDate is one index lookup:
--   user_id = ? AND all_steps_done AND deleted_at IS NULL
--   ORDER BY routine_date DESC LIMIT 1
-- A full-routine day is not a skin check and does not write the streak row.
-- Boot schema also comes from GORM AutoMigrate. Apply this file when
-- AutoMigrate is not the source of truth. The backfill and partial index
-- run from repository.applyRoutineFullDay on API boot.

ALTER TABLE routine_entries
    ADD COLUMN IF NOT EXISTS all_steps_done BOOLEAN NOT NULL DEFAULT FALSE;
