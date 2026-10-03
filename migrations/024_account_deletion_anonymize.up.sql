-- Account deletion nulls user_id on accounting / audit rows instead of
-- dropping the amounts and dates. Boot schema also comes from GORM AutoMigrate;
-- DeleteAccount repeats DROP NOT NULL on Postgres so a database that has not
-- applied this file can still detach those rows.
-- Apply by hand when AutoMigrate is not the source of truth.

ALTER TABLE payment_orders ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE plan_change_logs ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE plan_change_logs ALTER COLUMN actor_user_id DROP NOT NULL;
