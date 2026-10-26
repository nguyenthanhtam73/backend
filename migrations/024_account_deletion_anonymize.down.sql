-- Not reversed. Anonymized payment and plan-change rows may already have
-- NULL user ids, so restoring NOT NULL would fail and would re-link nothing
-- useful. Leave the columns nullable.
SELECT 1;
