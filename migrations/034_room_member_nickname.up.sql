-- Room persona for the diligence board. The live socket already cleaned it
-- (24 runes). 96 bytes covers that in UTF-8. Empty until the first room join
-- frame. This is not the account display name.
-- Boot schema also comes from GORM AutoMigrate.
-- Numbered 034 so it does not collide with the full-routine-day migration.

ALTER TABLE mascot_room_members
    ADD COLUMN IF NOT EXISTS nickname VARCHAR(96) NOT NULL DEFAULT '';
