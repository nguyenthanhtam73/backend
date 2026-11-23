ALTER TABLE mascot_rooms DROP CONSTRAINT IF EXISTS mascot_rooms_tree_type_fkey;
ALTER TABLE mascot_rooms DROP COLUMN IF EXISTS tree_type;

DROP TABLE IF EXISTS mascot_room_member_leaves;
DROP TABLE IF EXISTS mascot_room_tree_rewards;
DROP TABLE IF EXISTS mascot_room_tree_waters;
DROP TABLE IF EXISTS mascot_room_trees;
DROP TABLE IF EXISTS tree_types;

DELETE FROM user_mascot_items WHERE item_id IN (
    'tree_sticker_la_xanh',
    'tree_frame_hoa',
    'tree_clip_la_xanh',
    'tree_clip_hoa_anh_dao',
    'tree_vong_la_lo_hoi',
    'tree_no_hoa_huong_duong',
    'tree_clip_bo_cong_anh',
    'tree_clip_hoa_quynh',
    'tree_clip_sen_hong'
);

DELETE FROM mascot_items WHERE id IN (
    'tree_sticker_la_xanh',
    'tree_frame_hoa',
    'tree_clip_la_xanh',
    'tree_clip_hoa_anh_dao',
    'tree_vong_la_lo_hoi',
    'tree_no_hoa_huong_duong',
    'tree_clip_bo_cong_anh',
    'tree_clip_hoa_quynh',
    'tree_clip_sen_hong'
);

ALTER TABLE mascot_room_members DROP COLUMN IF EXISTS kind;
ALTER TABLE user_mascot_items DROP COLUMN IF EXISTS preview_only;
ALTER TABLE mascot_items DROP COLUMN IF EXISTS preview_only;
