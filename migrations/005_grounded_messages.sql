BEGIN;

ALTER TABLE app_conversations
    ADD COLUMN IF NOT EXISTS owner_team text,
    ADD COLUMN IF NOT EXISTS owner_role text;

UPDATE app_conversations AS conversation
SET owner_team = COALESCE(conversation.owner_team, owner.team_id),
    owner_role = COALESCE(conversation.owner_role, owner.role)
FROM app_users AS owner
WHERE conversation.owner_id = owner.id
    AND (conversation.owner_team IS NULL OR conversation.owner_role IS NULL);

ALTER TABLE app_conversations
    ALTER COLUMN owner_team SET NOT NULL,
    ALTER COLUMN owner_role SET NOT NULL;

ALTER TABLE app_messages
    ADD COLUMN IF NOT EXISTS reply_metadata jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS generation_token text,
    ADD COLUMN IF NOT EXISTS generation_until timestamptz;

CREATE INDEX IF NOT EXISTS app_messages_generation_idx
    ON app_messages (conversation_id, generation_until)
    WHERE generation_token IS NOT NULL;

COMMIT;
