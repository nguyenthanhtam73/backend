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
    ('body_default', 'body', 'Bé Da', TRUE, NULL),
    ('hair_default', 'hair', 'Tóc mềm', TRUE, NULL),
    ('outfit_default', 'outfit', 'Áo thun', TRUE, NULL),
    ('accessory_default', 'accessory', 'Nơ nhỏ', TRUE, NULL),
    ('room_plant', 'room', 'Cây nhỏ', TRUE, NULL)
ON CONFLICT (id) DO NOTHING;
