-- Cabinet product insight card, persisted on the wardrobe row.
-- Schema is also applied via GORM AutoMigrate in repository.AutoMigrate.

ALTER TABLE skincare_products
    ADD COLUMN IF NOT EXISTS insight JSONB,
    ADD COLUMN IF NOT EXISTS insight_at TIMESTAMPTZ;
