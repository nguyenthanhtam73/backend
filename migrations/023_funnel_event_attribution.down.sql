ALTER TABLE funnel_events
    DROP COLUMN IF EXISTS fbclid,
    DROP COLUMN IF EXISTS utm_content,
    DROP COLUMN IF EXISTS utm_campaign,
    DROP COLUMN IF EXISTS utm_source;
