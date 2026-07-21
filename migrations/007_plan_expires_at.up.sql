-- Paid plan expiry (SePay monthly/yearly). NULL = no expiry (admin lifetime grant / legacy).
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS plan_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_plan_expires_at
    ON users (plan_expires_at)
    WHERE plan_expires_at IS NOT NULL
      AND plan_tier IN ('premium', 'premium_plus');
