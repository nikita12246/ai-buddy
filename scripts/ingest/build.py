"""Reproduce the locked knowledge corpus and its PostgreSQL seed."""
import argparse
from hashlib import sha256
import json
from pathlib import Path
from tempfile import TemporaryDirectory

from .checklist import ROOT
from .corpus import build_corpus
from .seed import render_seed


def build(raw_dir, output_dir):
    lock = json.loads((ROOT / "data/corpus-lock.json").read_text(encoding="utf-8"))
    errors = []
    for prefix, base in (("raw", raw_dir), ("repo", ROOT)):
        for name, expected in lock[prefix].items():
            path = base / name
            if not path.is_file():
                errors.append(f"Missing input: {path}")
            elif sha256(path.read_bytes()).hexdigest() != expected:
                errors.append(f"Input does not match the locked snapshot: {path}")
    if errors:
        raise ValueError("\n".join(errors))

    output_dir.mkdir(parents=True, exist_ok=True)
    with TemporaryDirectory(dir=output_dir) as temporary:
        corpus = Path(temporary) / "corpus.jsonl"
        count = build_corpus(raw_dir, ROOT / "data/public-documents.json",
                             ROOT / "data/synthetic/policies.jsonl", corpus)
        digest = sha256(corpus.read_bytes()).hexdigest()
        if count != lock["chunk_count"] or digest != lock["corpus_sha256"]:
            raise ValueError(f"Unexpected corpus: {count} chunks, SHA-256 {digest}")
        seed = corpus.with_name("knowledge-seed.sql")
        seed.write_text(render_seed(corpus), encoding="utf-8")
        corpus.replace(output_dir / corpus.name)
        seed.replace(output_dir / seed.name)
    return count, digest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--raw-dir", type=Path, default=ROOT / "data/raw")
    parser.add_argument("--output-dir", type=Path, default=ROOT / "data/processed")
    args = parser.parse_args()
    try:
        count, digest = build(args.raw_dir, args.output_dir)
    except (OSError, ValueError) as error:
        parser.exit(1, f"{error}\n")
    print(f"Verified {count} chunks, SHA-256 {digest}")


if __name__ == "__main__":
    main()
