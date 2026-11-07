-- Drops the one-active-room seat and the end column.
-- Room rows and memberships stay.

DROP TABLE IF EXISTS mascot_room_active_seats;
DROP INDEX IF EXISTS mascot_rooms_ends_at_idx;
ALTER TABLE mascot_rooms DROP COLUMN IF EXISTS ends_at;
