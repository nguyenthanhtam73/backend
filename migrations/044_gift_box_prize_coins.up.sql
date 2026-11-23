-- Xu from a gift box is stored on the box so a replay shows the same amount.
-- Existing item opens stay at 0. AutoMigrate adds the same column on boot.

ALTER TABLE tree_gift_boxes
    ADD COLUMN IF NOT EXISTS prize_coins INTEGER NOT NULL DEFAULT 0;
