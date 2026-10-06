-- Account deletion nulls user_id on accounting / audit rows instead of
-- dropping the amounts and dates. GORM AutoMigrate cannot drop NOT NULL.
--
-- The API applies these statements at process startup
-- (repository.ApplyAccountDeletionSchema), after AutoMigrate and before it
-- listens for HTTP. On Railway that is the backend process using
-- DADIARY_DATABASE_URL; the deploy workflow does not run SQL files.
-- Nothing walks migrations/ in filename order, so 024 staying numbered
-- ahead of 025_reminder_engagement does not affect boot.
-- Apply this file by hand only when AutoMigrate is not the source of truth.
-- DELETE /me does not run these statements. If they have not been applied,
-- that endpoint returns 503 and leaves the account in place.

ALTER TABLE payment_orders ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE plan_change_logs ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE plan_change_logs ALTER COLUMN actor_user_id DROP NOT NULL;
