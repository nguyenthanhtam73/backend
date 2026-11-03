-- Bé Da invite rooms (phòng). One row per room, one row per member.
-- code is 6 uppercase characters with no 0, O, 1, or I.
-- max_members is 8. Live presence (including the 30s disconnect grace)
-- is in memory; this table is who is allowed in.
-- Boot schema also comes from GORM AutoMigrate. Apply this file on Postgres
-- when AutoMigrate is not the source of truth.
-- Item grants are not tied to this table. Deleting a room must not delete them.

CREATE TABLE IF NOT EXISTS mascot_rooms (
    code VARCHAR(6) PRIMARY KEY CHECK (code ~ '^[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{6}$'),
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    max_members INTEGER NOT NULL DEFAULT 8 CHECK (max_members BETWEEN 1 AND 8)
);

CREATE TABLE IF NOT EXISTS mascot_room_members (
    room_code VARCHAR(6) NOT NULL REFERENCES mascot_rooms(code) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_code, user_id)
);

CREATE INDEX IF NOT EXISTS mascot_room_members_user_idx ON mascot_room_members (user_id);
