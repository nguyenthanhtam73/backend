-- First-touch campaign values captured on POST /api/v1/auth/register.
-- Numbered 022 so it lands after 021_funnel_events.
-- Nullable. Do not store the account email in these columns.
-- Boot schema comes from GORM AutoMigrate in repository.AutoMigrate, which
-- does not read this directory. Apply this file by hand on Postgres when
-- that AutoMigrate is not the source of truth.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS utm_source   VARCHAR(200),
    ADD COLUMN IF NOT EXISTS utm_medium   VARCHAR(200),
    ADD COLUMN IF NOT EXISTS utm_campaign VARCHAR(200),
    ADD COLUMN IF NOT EXISTS utm_content  VARCHAR(200),
    ADD COLUMN IF NOT EXISTS fbclid       VARCHAR(200),
    ADD COLUMN IF NOT EXISTS ttclid       VARCHAR(200);
