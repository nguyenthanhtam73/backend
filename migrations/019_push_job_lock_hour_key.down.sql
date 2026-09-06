-- Only safe if no hour-shaped keys are stored. Production must stay on 16.
ALTER TABLE push_job_locks
    ALTER COLUMN last_run_date TYPE VARCHAR(10);
