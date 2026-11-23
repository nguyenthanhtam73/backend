-- Tree gift boxes. One unopened box per real member when a tree that has
-- a box blooms. Leaf is the only catalog in this version. The box is not
-- sold. Opening it is idempotent on the box id.
--
-- The old leaf clip stays tree_clip_la_xanh so existing owners keep it.
-- Its wear slot moves from accessory to hair_acc. App boot rewrites loadouts
-- that still wear it in the accessory list. This file updates the catalog
-- layer; it does not rewrite JSON.
--
-- Production boots through AutoMigrate and the Go seed. This file is the
-- same change for a database that applies SQL migrations. It does not
-- DELETE existing grants.

CREATE TABLE IF NOT EXISTS tree_gift_boxes (
    id UUID PRIMARY KEY,
    room_code VARCHAR(6) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tree_type VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    opened_at TIMESTAMPTZ,
    prize_id VARCHAR(64) NOT NULL DEFAULT '',
    granted_item_ids VARCHAR(256) NOT NULL DEFAULT '',
    dupe_coins INTEGER NOT NULL DEFAULT 0,
    UNIQUE (room_code, user_id)
);

CREATE INDEX IF NOT EXISTS tree_gift_boxes_user_idx ON tree_gift_boxes (user_id);

UPDATE mascot_items
SET layer = 'hair_acc'
WHERE id = 'tree_clip_la_xanh' AND layer = 'accessory';

INSERT INTO mascot_items (id, layer, name, is_free, price, preview_only) VALUES
    ('top_la_non', 'top', 'Áo thun Lá Non', FALSE, NULL, FALSE),
    ('bottom_quan_short_kem', 'bottom', 'Quần short Kem Sữa', FALSE, NULL, FALSE),
    ('top_ao_len_suong_som', 'top', 'Áo len Sương Sớm', FALSE, NULL, FALSE),
    ('bottom_vay_xep_ly_ma_non', 'bottom', 'Váy xếp ly Mạ Non', FALSE, NULL, FALSE),
    ('top_ao_co_la', 'top', 'Áo cổ Lá Xinh', FALSE, NULL, FALSE),
    ('bottom_quan_dai_reu_nhat', 'bottom', 'Quần dài Rêu Nhạt', FALSE, NULL, FALSE),
    ('top_ao_ke_la_me', 'top', 'Áo kẻ Lá Me', FALSE, NULL, FALSE),
    ('bottom_chan_vay_la_dam', 'bottom', 'Chân váy Lá Đậm', FALSE, NULL, FALSE),
    ('top_ao_dao_vuon', 'top', 'Áo túi lá Dạo Vườn', FALSE, NULL, FALSE),
    ('bottom_quan_lung_kaki', 'bottom', 'Quần lửng Kaki và ủng xanh', FALSE, NULL, FALSE),
    ('top_ao_tay_phong_giot_suong', 'top', 'Áo tay phồng Giọt Sương', FALSE, NULL, FALSE),
    ('bottom_vay_tang_la_biec', 'bottom', 'Váy tầng Lá Biếc', FALSE, NULL, FALSE),
    ('top_ao_khoac_rung_nho', 'top', 'Áo khoác Rừng Nhỏ', FALSE, NULL, FALSE),
    ('bottom_quan_ong_rong_la_theu', 'bottom', 'Quần ống rộng Lá Thêu', FALSE, NULL, FALSE),
    ('top_ao_la_ngoc', 'top', 'Áo Lá Ngọc', FALSE, NULL, FALSE),
    ('bottom_vay_xoe_la_ngoc', 'bottom', 'Váy xoè Lá Ngọc', FALSE, NULL, FALSE),
    ('acc_sticker_ma_la', 'cheek', 'Sticker má Lá Nhỏ', FALSE, NULL, FALSE),
    ('acc_kinh_tron_la_ma', 'glasses', 'Kính tròn Lá Mạ', FALSE, NULL, FALSE),
    ('acc_vong_tay_hat_la', 'bracelet', 'Vòng tay Hạt Lá', FALSE, NULL, FALSE),
    ('acc_no_la_xanh', 'hair_acc', 'Nơ Lá Xanh', FALSE, NULL, FALSE),
    ('acc_khuyen_tai_giot_suong', 'earring', 'Khuyên tai Giọt Sương', FALSE, NULL, FALSE),
    ('acc_tui_la_biec', 'bag', 'Túi Lá Biếc', FALSE, NULL, FALSE),
    ('acc_vuong_mien_la_ngoc', 'hat', 'Vương miện Lá Ngọc', FALSE, NULL, FALSE)
ON CONFLICT (id) DO NOTHING;
