-- Hourly D0/D1 job claims with "2006-01-02-15" (13 chars). The original
-- last_run_date VARCHAR(10) was sized for civil-day keys only; Postgres
-- rejected those INSERTs (SQLSTATE 22001) so the in-process hourly job
-- never ran after boot. Also applied via GORM AutoMigrate (size:16).

ALTER TABLE push_job_locks
    ALTER COLUMN last_run_date TYPE VARCHAR(16);
