-- Shared room tree (Cây chung).
-- tree_types is data. There is no enum and no CHECK on mascot_rooms.tree_type.
-- A later tree is a new tree_types row plus its item and art, not a code change.
-- Rooms reference tree_types.key. Existing rooms become leaf via the default.
-- This version plants leaf only. cherry, aloe, and sunflower are enabled so
-- the client can draw them. dandelion, quynh, and sen_hong are rare and disabled.
--
-- Waters are unique per (room, user, Vietnam date). Stage rewards are unique
-- per (room, user, stage). Coin credits use user_coin_ledger (reason tree_stage).
-- Wardrobe grants live in user_mascot_items and are not deleted when a room ends.
-- preview_only marks pieces that can be owned but not worn yet.
-- member kind distinguishes a future virtual buddy from a real account.
-- A leave row keeps the 00:00 membership after the member row is gone.

ALTER TABLE mascot_items
    ADD COLUMN IF NOT EXISTS preview_only BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE user_mascot_items
    ADD COLUMN IF NOT EXISTS preview_only BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE mascot_room_members
    ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'user';

INSERT INTO mascot_items (id, layer, name, is_free, price, preview_only) VALUES
    ('tree_sticker_la_xanh', 'sticker', 'Nhãn dán lá xanh', FALSE, NULL, TRUE),
    ('tree_frame_hoa', 'name_frame', 'Khung tên hoa', FALSE, NULL, TRUE),
    ('tree_clip_la_xanh', 'accessory', 'Kẹp tóc lá xanh', FALSE, NULL, FALSE),
    ('tree_clip_hoa_anh_dao', 'accessory', 'Kẹp hoa anh đào', FALSE, NULL, FALSE),
    ('tree_vong_la_lo_hoi', 'accessory', 'Vòng lá lô hội', FALSE, NULL, FALSE),
    ('tree_no_hoa_huong_duong', 'accessory', 'Nơ hoa hướng dương', FALSE, NULL, FALSE),
    ('tree_clip_bo_cong_anh', 'accessory', 'Kẹp hoa bồ công anh', FALSE, NULL, FALSE),
    ('tree_clip_hoa_quynh', 'accessory', 'Kẹp hoa quỳnh', FALSE, NULL, FALSE),
    ('tree_clip_sen_hong', 'accessory', 'Kẹp sen hồng', FALSE, NULL, FALSE)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS tree_types (
    key VARCHAR(64) PRIMARY KEY,
    name_vi VARCHAR(128) NOT NULL,
    rarity VARCHAR(16) NOT NULL,
    exclusive_item_key VARCHAR(64) NOT NULL REFERENCES mascot_items(id),
    image_key VARCHAR(64) NOT NULL,
    sort_order INTEGER NOT NULL,
    enabled BOOLEAN NOT NULL
);

INSERT INTO tree_types (key, name_vi, rarity, exclusive_item_key, image_key, sort_order, enabled) VALUES
    ('leaf', 'Cây lá xanh', 'common', 'tree_clip_la_xanh', 'leaf', 1, TRUE),
    ('cherry', 'Hoa anh đào', 'common', 'tree_clip_hoa_anh_dao', 'cherry', 2, TRUE),
    ('aloe', 'Lô hội', 'common', 'tree_vong_la_lo_hoi', 'aloe', 3, TRUE),
    ('sunflower', 'Hướng dương', 'common', 'tree_no_hoa_huong_duong', 'sunflower', 4, TRUE),
    ('dandelion', 'Bồ công anh', 'rare', 'tree_clip_bo_cong_anh', 'dandelion', 5, FALSE),
    ('quynh', 'Hoa quỳnh', 'rare', 'tree_clip_hoa_quynh', 'quynh', 6, FALSE),
    ('sen_hong', 'Sen hồng', 'rare', 'tree_clip_sen_hong', 'sen_hong', 7, FALSE)
ON CONFLICT (key) DO NOTHING;

ALTER TABLE mascot_rooms
    ADD COLUMN IF NOT EXISTS tree_type VARCHAR(64) NOT NULL DEFAULT 'leaf';

UPDATE mascot_rooms SET tree_type = 'leaf' WHERE tree_type IS NULL OR tree_type = '';

DO $$ BEGIN
    ALTER TABLE mascot_rooms
        ADD CONSTRAINT mascot_rooms_tree_type_fkey
        FOREIGN KEY (tree_type) REFERENCES tree_types(key);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS mascot_room_trees (
    room_code VARCHAR(6) PRIMARY KEY REFERENCES mascot_rooms(code) ON DELETE CASCADE,
    stage INTEGER NOT NULL DEFAULT 1,
    progress_num BIGINT NOT NULL DEFAULT 0,
    progress_den BIGINT NOT NULL DEFAULT 1,
    leveled_on VARCHAR(10) NOT NULL DEFAULT '',
    settled_through VARCHAR(10) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS mascot_room_tree_waters (
    room_code VARCHAR(6) NOT NULL REFERENCES mascot_rooms(code) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    vn_date VARCHAR(10) NOT NULL,
    idempotency_key VARCHAR(80) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_code, user_id, vn_date)
);

CREATE INDEX IF NOT EXISTS idx_tree_water_day ON mascot_room_tree_waters (room_code, vn_date);

CREATE TABLE IF NOT EXISTS mascot_room_tree_rewards (
    room_code VARCHAR(6) NOT NULL REFERENCES mascot_rooms(code) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stage INTEGER NOT NULL,
    coins INTEGER NOT NULL DEFAULT 0,
    item_id VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_code, user_id, stage)
);

CREATE TABLE IF NOT EXISTS mascot_room_member_leaves (
    room_code VARCHAR(6) NOT NULL REFERENCES mascot_rooms(code) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL,
    left_at TIMESTAMPTZ NOT NULL,
    kind VARCHAR(16) NOT NULL DEFAULT 'user',
    PRIMARY KEY (room_code, user_id, joined_at)
);

CREATE INDEX IF NOT EXISTS mascot_room_member_leaves_left_idx ON mascot_room_member_leaves (left_at);
