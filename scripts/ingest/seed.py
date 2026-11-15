"""Render a transactional PostgreSQL seed from the prepared JSONL corpus."""
import argparse
import json
from pathlib import Path

from .checklist import ROOT

COLUMNS = {
    "chunk_id": "text",
    "document_id": "text",
    "title": "text",
    "text": "text",
    "source_url": "text",
    "version": "text",
    "source_id": "text",
    "scope": "text",
    "team_id": "text",
    "allowed_roles": "text[]",
    "source_kind": "text",
}


def render_seed(input_path):
    with input_path.open(encoding="utf-8") as file:
        rows = [json.loads(line) for line in file if line.strip()]
    if not rows:
        raise ValueError("Refusing to seed an empty corpus")
    for row in rows:
        row.setdefault("allowed_roles", [])

    # Hex-encode the payload so source text cannot escape the SQL literal.
    payload = json.dumps(rows, ensure_ascii=False).encode("utf-8").hex()
    columns = ", ".join(COLUMNS)
    types = ", ".join(f"{key} {value}" for key, value in COLUMNS.items())
    update = ",\n    ".join(f"{key} = EXCLUDED.{key}" for key in COLUMNS if key != "chunk_id")
    return f"""BEGIN;

CREATE TEMP TABLE incoming_knowledge ON COMMIT DROP AS
SELECT {columns}
FROM jsonb_to_recordset(convert_from(decode('{payload}', 'hex'), 'UTF8')::jsonb)
AS incoming({types});

DELETE FROM knowledge_chunks AS existing
WHERE existing.source_id IN (SELECT source_id FROM incoming_knowledge)
  AND existing.chunk_id NOT IN (SELECT chunk_id FROM incoming_knowledge);

INSERT INTO knowledge_chunks ({columns})
SELECT {columns} FROM incoming_knowledge
ON CONFLICT (chunk_id) DO UPDATE SET
    {update};

COMMIT;
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, default=ROOT / "data/processed/corpus.jsonl")
    args = parser.parse_args()
    print(render_seed(args.input), end="")


if __name__ == "__main__":
    main()
