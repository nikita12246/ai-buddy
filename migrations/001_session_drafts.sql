BEGIN;

CREATE TABLE IF NOT EXISTS app_users (
    id text PRIMARY KEY,
    team_id text NOT NULL,
    role text NOT NULL,
    name text NOT NULL
);

CREATE TABLE IF NOT EXISTS app_sessions (
    user_id text PRIMARY KEY REFERENCES app_users (id),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS app_resources (
    team_id text NOT NULL,
    tool_id text NOT NULL,
    resource_id text NOT NULL,
    PRIMARY KEY (team_id, tool_id, resource_id)
);

CREATE TABLE IF NOT EXISTS app_conversations (
    id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES app_users (id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS app_conversations_owner_id_idx ON app_conversations (owner_id);

CREATE TABLE IF NOT EXISTS app_messages (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversation_id text NOT NULL REFERENCES app_conversations (id),
    client_message_id text NOT NULL,
    role text NOT NULL CHECK (role IN ('user', 'assistant')),
    text text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, client_message_id, role)
);

CREATE INDEX IF NOT EXISTS app_messages_conversation_id_id_idx ON app_messages (conversation_id, id);

CREATE TABLE IF NOT EXISTS app_drafts (
    id text PRIMARY KEY,
    conversation_id text NOT NULL UNIQUE REFERENCES app_conversations (id),
    tool_id text NOT NULL,
    resource_id text NOT NULL,
    justification text NOT NULL DEFAULT '',
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS app_operations (
    id text PRIMARY KEY,
    draft_id text NOT NULL UNIQUE REFERENCES app_drafts (id),
    confirmed_version integer NOT NULL CHECK (confirmed_version > 0),
    idempotency_key text NOT NULL UNIQUE,
    immutable_payload jsonb NOT NULL,
    delivery_state text NOT NULL DEFAULT 'queued'
        CHECK (delivery_state IN ('queued', 'sending', 'delivered', 'reconciling')),
    external_id text,
    service_status text CHECK (service_status IN ('pending_approval', 'approved', 'rejected')),
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMIT;
