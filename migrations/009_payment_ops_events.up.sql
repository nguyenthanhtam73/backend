-- Ops event log for payment webhook errors (admin metrics + fail-rate alerts).
CREATE TABLE IF NOT EXISTS payment_ops_events (
    id UUID PRIMARY KEY,
    kind VARCHAR(32) NOT NULL,
    reason VARCHAR(64) NOT NULL DEFAULT '',
    invoice_number VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payment_ops_events_kind_created
    ON payment_ops_events (kind, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_payment_ops_events_created
    ON payment_ops_events (created_at DESC);
