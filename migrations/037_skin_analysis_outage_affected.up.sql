-- Keeps a check in the apology cohort after a replay marks the analysis completed.
-- Existing rows stay false. Not a data rewrite.

ALTER TABLE skin_analyses
    ADD COLUMN IF NOT EXISTS outage_affected BOOLEAN NOT NULL DEFAULT FALSE;
