-- Room for one-shot markers such as ai_outage_apology_20261106.
-- Existing d0/d1/d3 values are unchanged.

ALTER TABLE email_send_receipts
    ALTER COLUMN kind TYPE VARCHAR(64);
