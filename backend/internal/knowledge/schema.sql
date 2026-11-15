CREATE TABLE IF NOT EXISTS knowledge_chunks (
    chunk_id text PRIMARY KEY,
    document_id text NOT NULL,
    title text NOT NULL,
    text text NOT NULL,
    source_url text,
    version text NOT NULL,
    source_id text NOT NULL,
    scope text NOT NULL CHECK (scope IN ('public', 'team')),
    team_id text,
    allowed_roles text[] NOT NULL DEFAULT '{}',
    source_kind text NOT NULL CONSTRAINT knowledge_chunks_source_kind_v2_check
        CHECK (source_kind IN ('avito_public', 'demo_policy', 'vendor_docs')),
    search_vector tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('pg_catalog.russian', title), 'A') ||
        setweight(to_tsvector('pg_catalog.russian', text), 'B')
    ) STORED,
    CHECK (
        (scope = 'public' AND team_id IS NULL)
        OR (scope = 'team' AND team_id IS NOT NULL AND btrim(team_id) <> '')
    )
);

-- Upgrade databases seeded by the first version of this schema.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'knowledge_chunks'::regclass
          AND conname = 'knowledge_chunks_source_kind_v2_check'
    ) THEN
        ALTER TABLE knowledge_chunks
            DROP CONSTRAINT IF EXISTS knowledge_chunks_source_kind_check,
            ADD CONSTRAINT knowledge_chunks_source_kind_v2_check
                CHECK (source_kind IN ('avito_public', 'demo_policy', 'vendor_docs'));
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS knowledge_chunks_search_vector_idx
    ON knowledge_chunks USING GIN (search_vector);
