-- Attribution columns on funnel_events, same shape as the user UTM fields.
-- Numbered 023 so it lands after 022_user_register_attribution.
-- Nullable. Do not store an email in these columns.
-- UTM columns are VARCHAR(100). fbclid is VARCHAR(256), matching the frontend
-- truncation and users.fbclid. Values that fail validation are dropped before
-- insert. utm_content is what our Meta ads set (video_tu_do); Meta also appends
-- fbclid.
-- Boot schema comes from GORM AutoMigrate in repository.AutoMigrate, which
-- does not read this directory. Apply this file by hand on Postgres when
-- that AutoMigrate is not the source of truth.

ALTER TABLE funnel_events
    ADD COLUMN IF NOT EXISTS utm_source   VARCHAR(100),
    ADD COLUMN IF NOT EXISTS utm_campaign VARCHAR(100),
    ADD COLUMN IF NOT EXISTS utm_content  VARCHAR(100),
    ADD COLUMN IF NOT EXISTS fbclid       VARCHAR(256);
