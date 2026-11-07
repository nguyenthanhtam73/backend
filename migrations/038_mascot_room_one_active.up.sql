-- One active Góc bình yên room per account, and an end time on every room.
-- Numbered 038 so it does not collide with the outage migrations 035–037.
-- New rooms last 8 days (domain.MascotRoomDuration). The API writes that
-- end on insert. This file does not delete rooms or memberships.
--
-- Existing rows had no end column. A missing end is
-- LEAST(created_at + 21 days, NOW() + 8 days). That is at most 8 days from
-- this statement, and it is not later than created_at + 21 days, so a room
-- already past that 21-day end stays ended. A real stored end is not
-- rewritten here. Nothing is deleted.
--
-- mascot_room_active_seats is one row per account (primary key user_id).
-- Two concurrent create/join transactions cannot both commit a seat.
-- Accounts that already belong to more than one still-active room get no
-- seat. Those memberships stay. Create and join refuse them until they
-- are in zero active rooms. Do not delete those rows here.
--
-- Boot schema also comes from GORM AutoMigrate. repository.prepareMascotRoomEnds
-- and repository.applyMascotRoomTerm do the same backfill when this file
-- is not the source of truth.

ALTER TABLE mascot_rooms
    ADD COLUMN IF NOT EXISTS ends_at TIMESTAMPTZ;

UPDATE mascot_rooms
SET ends_at = LEAST(created_at + INTERVAL '21 days', NOW() + INTERVAL '8 days')
WHERE ends_at IS NULL OR ends_at < TIMESTAMPTZ '1971-01-01';

ALTER TABLE mascot_rooms
    ALTER COLUMN ends_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS mascot_rooms_ends_at_idx ON mascot_rooms (ends_at);

CREATE TABLE IF NOT EXISTS mascot_room_active_seats (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    room_code VARCHAR(6) NOT NULL REFERENCES mascot_rooms(code) ON DELETE CASCADE,
    ends_at TIMESTAMPTZ NOT NULL
);

-- Seat only the accounts with exactly one active room. Everyone else is left
-- alone, including accounts in several active rooms and accounts in none.
INSERT INTO mascot_room_active_seats (user_id, room_code, ends_at)
SELECT m.user_id, m.room_code, r.ends_at
FROM mascot_room_members m
JOIN mascot_rooms r ON r.code = m.room_code
WHERE r.ends_at > NOW()
  AND NOT EXISTS (
      SELECT 1 FROM mascot_room_active_seats s WHERE s.user_id = m.user_id
  )
  AND m.user_id IN (
      SELECT m2.user_id
      FROM mascot_room_members m2
      JOIN mascot_rooms r2 ON r2.code = m2.room_code
      WHERE r2.ends_at > NOW()
      GROUP BY m2.user_id
      HAVING COUNT(*) = 1
  );
