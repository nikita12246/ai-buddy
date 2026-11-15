"""Import the two supplied Avito onboarding checklist tabs into JSONL."""
import argparse
import csv
import hashlib
import io
import json
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SHEET_URL = "https://docs.google.com/spreadsheets/d/1qKFqOzfSZ2jEz57jBSmq7rukDXi_tRfWohgemViceV0/edit"
TITLE = "Материалы чек-листа"
DESCRIPTION = "Описание пункта"
PERIOD = "Период прохождения задачи"
TABS = (
    ("general", "Материалы для всех", "1073060303", (TITLE, DESCRIPTION, PERIOD)),
    ("backend", "Чек-лист Backend", "0", (TITLE, PERIOD)),
)


def read_tab(input_dir, slug, sheet, gid, columns):
    path = input_dir / f"{slug}.csv"
    raw = path.read_bytes()
    version = f"sha256:{hashlib.sha256(raw).hexdigest()}"
    rows = csv.reader(io.StringIO(raw.decode("utf-8-sig"), newline=""), strict=True)
    header = tuple(" ".join(cell.split()) for cell in next(rows, ()))
    if header != columns:
        raise ValueError(f"{path.name}: unexpected columns {header!r}; expected {columns!r}")

    document_id = f"avito-checklist-{slug}"
    last_column = chr(ord("A") + len(columns) - 1)
    for row_number, row in enumerate(rows, start=2):
        values = [cell.strip() for cell in row]
        if not any(values):
            continue
        if len(values) != len(columns) or not values[0]:
            raise ValueError(f"{path.name}:{row_number}: malformed checklist row")

        title = values[0].splitlines()[0].strip()
        text = "\n\n".join(value for value in values[:-1] if value)
        period = values[-1]
        if period:
            text += f"\n\nПериод прохождения задачи — {period}."

        yield {
            "chunk_id": f"{document_id}-r{row_number}",
            "document_id": document_id,
            "title": title,
            "text": text,
            "source_url": f"{SHEET_URL}#gid={gid}&range=A{row_number}:{last_column}{row_number}",
            "version": version,
            "source_id": "avito-checklist",
            "sheet": sheet,
            "row": row_number,
            "period": period or None,
            "scope": "public",
            "source_kind": "avito_public",
        }


def write_jsonl(output, chunks):
    output.parent.mkdir(parents=True, exist_ok=True)
    staging = tempfile.NamedTemporaryFile(
        mode="w", encoding="utf-8", newline="\n", dir=output.parent, delete=False
    )
    staging_path = Path(staging.name)
    try:
        with staging:
            for chunk in chunks:
                staging.write(json.dumps(chunk, ensure_ascii=False) + "\n")
        staging_path.replace(output)
    finally:
        staging_path.unlink(missing_ok=True)


def import_checklist(input_dir, output):
    chunks = [chunk for tab in TABS for chunk in read_tab(input_dir, *tab)]
    write_jsonl(output, chunks)
    return len(chunks)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input-dir", type=Path, default=ROOT / "data/raw/avito-checklist")
    parser.add_argument("--output", type=Path, default=ROOT / "data/processed/avito-checklist.jsonl")
    args = parser.parse_args()
    try:
        count = import_checklist(args.input_dir, args.output)
    except (OSError, ValueError, csv.Error) as error:
        parser.exit(1, f"Checklist import failed: {error}\n")
    print(f"Imported {count} checklist rows into {args.output}")


if __name__ == "__main__":
    main()
