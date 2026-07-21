-- SePay checkout orders (created before redirect; updated by IPN webhook).
-- Schema is also applied via GORM AutoMigrate in repository.AutoMigrate.

CREATE TABLE IF NOT EXISTS payment_orders (
    id                    UUID PRIMARY KEY,
    user_id               UUID NOT NULL,
    invoice_number        VARCHAR(64) NOT NULL,
    plan_tier             VARCHAR(16) NOT NULL,
    billing_interval      VARCHAR(16) NOT NULL,
    amount_vnd            BIGINT NOT NULL,
    currency              VARCHAR(8) NOT NULL DEFAULT 'VND',
    status                VARCHAR(16) NOT NULL DEFAULT 'pending',
    provider              VARCHAR(16) NOT NULL DEFAULT 'sepay',
    se_pay_order_id       VARCHAR(128),
    se_pay_transaction_id VARCHAR(128),
    custom_data           TEXT,
    raw_webhook           TEXT,
    paid_at               TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at            TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_orders_invoice_number
    ON payment_orders (invoice_number);

CREATE INDEX IF NOT EXISTS idx_payment_orders_user_id
    ON payment_orders (user_id);

CREATE INDEX IF NOT EXISTS idx_payment_orders_status
    ON payment_orders (status);

CREATE INDEX IF NOT EXISTS idx_payment_orders_deleted_at
    ON payment_orders (deleted_at);
