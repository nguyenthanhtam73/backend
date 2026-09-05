-- Idempotent repair: close subscription rows that stayed "active" after
-- period_ends_at. Prefer cmd/reconcile-subscriptions --apply (also syncs users).
-- This SQL is the emergency fallback if the Go command cannot run.
-- Schema is also applied via GORM AutoMigrate; this file is data-only.

UPDATE subscriptions
SET status = CASE
      WHEN canceled_at IS NOT NULL THEN 'canceled'
      WHEN period_ends_at + INTERVAL '3 days' > NOW() THEN 'past_due'
      ELSE 'expired'
    END,
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND status IN ('active', 'trialing', 'canceled', 'past_due')
  AND period_ends_at IS NOT NULL
  AND period_ends_at <= NOW()
  AND status <> CASE
      WHEN canceled_at IS NOT NULL THEN 'canceled'
      WHEN period_ends_at + INTERVAL '3 days' > NOW() THEN 'past_due'
      ELSE 'expired'
    END;
