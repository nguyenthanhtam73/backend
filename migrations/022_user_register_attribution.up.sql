-- First-touch campaign values captured on POST /api/v1/auth/register.
-- Numbered 022 so it lands after 021_funnel_events.
-- Nullable. Do not store the account email in these columns.
-- UTM columns are VARCHAR(100). fbclid is VARCHAR(256) so it matches the
-- frontend truncation. ttclid is VARCHAR(255).
-- Values that fail validation are dropped in the handler before insert.
-- Boot schema comes from GORM AutoMigrate in repository.AutoMigrate, which
-- does not read this directory. Apply this file by hand on Postgres when
-- that AutoMigrate is not the source of truth.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS utm_source   VARCHAR(100),
    ADD COLUMN IF NOT EXISTS utm_medium   VARCHAR(100),
    ADD COLUMN IF NOT EXISTS utm_campaign VARCHAR(100),
    ADD COLUMN IF NOT EXISTS utm_content  VARCHAR(100),
    ADD COLUMN IF NOT EXISTS fbclid       VARCHAR(256),
    ADD COLUMN IF NOT EXISTS ttclid       VARCHAR(255);

-- Widen fbclid when an earlier draft of this migration created VARCHAR(255).
ALTER TABLE users ALTER COLUMN fbclid TYPE VARCHAR(256);
