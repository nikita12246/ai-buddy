BEGIN;

CREATE SCHEMA IF NOT EXISTS mock_access;

CREATE TABLE IF NOT EXISTS mock_access.requests (
    id text PRIMARY KEY,
    idempotency_key text NOT NULL UNIQUE,
    payload_hash bytea NOT NULL CHECK (octet_length(payload_hash) = 32),
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending_approval'
        CHECK (status IN ('pending_approval', 'approved', 'rejected')),
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMIT;
