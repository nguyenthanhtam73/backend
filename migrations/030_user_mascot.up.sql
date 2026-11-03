-- Bé Da: one mascot row per user, plus a small item catalog.
-- loadout is JSON so a future shop can add items without a schema change:
-- {"body","hair","outfit","accessory","room_items":[]}.
-- Mood is not stored; GET derives it from check-ins and the routine checklist.
-- Boot schema also comes from GORM AutoMigrate. Apply this file on Postgres
-- when AutoMigrate is not the source of truth.
-- Keep the seeded ids in sync with domain.DefaultMascotItems.

CREATE TABLE IF NOT EXISTS mascot_items (
    id VARCHAR(64) PRIMARY KEY,
    layer VARCHAR(32) NOT NULL,
    name VARCHAR(128) NOT NULL,
    is_free BOOLEAN NOT NULL DEFAULT FALSE,
    price INTEGER
);

CREATE TABLE IF NOT EXISTS user_mascot (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    loadout JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO mascot_items (id, layer, name, is_free, price) VALUES
    ('body_chibi', 'body', 'Bé Da', TRUE, NULL),
    ('hair_sleep_buns', 'hair', 'Tóc búi ngủ', TRUE, NULL),
    ('outfit_cozy_pajamas', 'outfit', 'Pyjama hồng', TRUE, NULL),
    ('accessory_star_clip', 'accessory', 'Kẹp sao', TRUE, NULL),
    ('room_moon', 'room', 'Trăng', TRUE, NULL),
    ('room_plant', 'room', 'Cây nhỏ', TRUE, NULL),
    ('room_pillow', 'room', 'Gối', TRUE, NULL)
ON CONFLICT (id) DO NOTHING;

-- Owned items belong to the user, not to a room. source_room_id is a label
-- only (no foreign key), so closing a room must not delete grants.
-- expires_at NULL means permanent. Readers filter expires_at > now(); no cron.
-- Free defaults are inserted per user on first mascot read, not here.
CREATE TABLE IF NOT EXISTS user_mascot_items (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id VARCHAR(64) NOT NULL REFERENCES mascot_items(id),
    source VARCHAR(32) NOT NULL,
    source_room_id VARCHAR(64),
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, item_id)
);
