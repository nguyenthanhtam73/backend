-- Lease column so only one process generates an onboarding starter routine.
-- Empty token means the row is free. Not a data rewrite.

ALTER TABLE skin_profiles
    ADD COLUMN IF NOT EXISTS starter_claim_token VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE skin_profiles
    ADD COLUMN IF NOT EXISTS starter_claim_at TIMESTAMPTZ;
