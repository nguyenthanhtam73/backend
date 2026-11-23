DROP TABLE IF EXISTS tree_gift_boxes;

DELETE FROM user_mascot_items WHERE item_id IN (
    'top_la_non', 'bottom_quan_short_kem',
    'top_ao_len_suong_som', 'bottom_vay_xep_ly_ma_non',
    'top_ao_co_la', 'bottom_quan_dai_reu_nhat',
    'top_ao_ke_la_me', 'bottom_chan_vay_la_dam',
    'top_ao_dao_vuon', 'bottom_quan_lung_kaki',
    'top_ao_tay_phong_giot_suong', 'bottom_vay_tang_la_biec',
    'top_ao_khoac_rung_nho', 'bottom_quan_ong_rong_la_theu',
    'top_ao_la_ngoc', 'bottom_vay_xoe_la_ngoc',
    'acc_sticker_ma_la', 'acc_kinh_tron_la_ma', 'acc_vong_tay_hat_la', 'acc_no_la_xanh',
    'acc_khuyen_tai_giot_suong', 'acc_tui_la_biec', 'acc_vuong_mien_la_ngoc'
);

DELETE FROM mascot_items WHERE id IN (
    'top_la_non', 'bottom_quan_short_kem',
    'top_ao_len_suong_som', 'bottom_vay_xep_ly_ma_non',
    'top_ao_co_la', 'bottom_quan_dai_reu_nhat',
    'top_ao_ke_la_me', 'bottom_chan_vay_la_dam',
    'top_ao_dao_vuon', 'bottom_quan_lung_kaki',
    'top_ao_tay_phong_giot_suong', 'bottom_vay_tang_la_biec',
    'top_ao_khoac_rung_nho', 'bottom_quan_ong_rong_la_theu',
    'top_ao_la_ngoc', 'bottom_vay_xoe_la_ngoc',
    'acc_sticker_ma_la', 'acc_kinh_tron_la_ma', 'acc_vong_tay_hat_la', 'acc_no_la_xanh',
    'acc_khuyen_tai_giot_suong', 'acc_tui_la_biec', 'acc_vuong_mien_la_ngoc'
);

UPDATE mascot_items
SET layer = 'accessory'
WHERE id = 'tree_clip_la_xanh' AND layer = 'hair_acc';
