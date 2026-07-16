-- Once-per-day claims for evening push jobs (multi-replica / restart safe).
-- Also applied via GORM AutoMigrate in repository.AutoMigrate.

CREATE TABLE IF NOT EXISTS push_job_locks (
    job_name      VARCHAR(64) PRIMARY KEY,
    last_run_date VARCHAR(10) NOT NULL DEFAULT '',
    claimed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
