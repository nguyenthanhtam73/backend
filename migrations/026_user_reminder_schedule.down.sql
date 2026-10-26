ALTER TABLE users
    DROP COLUMN IF EXISTS reminder_timezone,
    DROP COLUMN IF EXISTS reminder_time,
    DROP COLUMN IF EXISTS reminder_enabled;
