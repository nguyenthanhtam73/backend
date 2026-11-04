DROP INDEX IF EXISTS idx_routine_entries_full_day_live;
DROP INDEX IF EXISTS idx_routine_entries_user_full_day;
ALTER TABLE routine_entries DROP COLUMN IF EXISTS all_steps_done;
