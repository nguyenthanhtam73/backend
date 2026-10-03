ALTER TABLE users
    DROP COLUMN IF EXISTS ttclid,
    DROP COLUMN IF EXISTS fbclid,
    DROP COLUMN IF EXISTS utm_content,
    DROP COLUMN IF EXISTS utm_campaign,
    DROP COLUMN IF EXISTS utm_medium,
    DROP COLUMN IF EXISTS utm_source;
