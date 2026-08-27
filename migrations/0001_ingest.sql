-- Every accepted transaction lands here exactly once. The UNIQUE
-- constraint on idempotency_key is the real enforcement, same pattern as
-- SPREAD's ledger: an application-level check races are possible, the
-- database constraint is what actually can't be beaten.
CREATE TABLE IF NOT EXISTS transactions (
    id                BIGSERIAL PRIMARY KEY,
    idempotency_key   TEXT NOT NULL UNIQUE,
    bank              TEXT NOT NULL,
    native_id         TEXT NOT NULL,
    account_id        TEXT NOT NULL,
    amount_cents      BIGINT NOT NULL,
    currency          TEXT NOT NULL,
    merchant_name     TEXT NOT NULL,
    merchant_category TEXT NOT NULL,
    occurred_at       TIMESTAMPTZ NOT NULL,
    ingested_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS transactions_account_idx ON transactions(account_id, occurred_at);
