BEGIN;

ALTER TABLE app_operations
    ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    ADD COLUMN IF NOT EXISTS lease_until timestamptz,
    ADD COLUMN IF NOT EXISTS lease_token text,
    ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS app_operations_due_idx
    ON app_operations (next_attempt_at, id)
    WHERE delivery_state IN ('queued', 'sending', 'reconciling')
       OR (delivery_state = 'delivered' AND service_status = 'pending_approval');

COMMIT;
