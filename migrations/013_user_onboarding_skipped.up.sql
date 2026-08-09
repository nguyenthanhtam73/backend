-- Persist “skip onboarding / enter app” across devices (not only localStorage).
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS onboarding_skipped BOOLEAN NOT NULL DEFAULT FALSE;
