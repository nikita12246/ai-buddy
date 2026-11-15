"""Prepare answer checks with known public evidence for the AI runner."""
import argparse
import json
from pathlib import Path

from .checklist import ROOT


def prepare(corpus_path):
    corpus = {item["chunk_id"]: item for item in (
        json.loads(line) for line in corpus_path.read_text(encoding="utf-8").splitlines()
        if line.strip()
    )}
    selections = {
        "retrieval-cases.json": {"retrieval-03", "retrieval-04", "retrieval-09", "retrieval-11"},
        "retrieval-expansion.json": {f"sprint-{index:02}" for index in (1, 3, 5, 7, 9, 12)},
    }
    cases = []
    for name, selected in selections.items():
        source_cases = json.loads((ROOT / "evals" / name).read_text(encoding="utf-8"))
        found = {case["id"] for case in source_cases}
        if selected - found:
            raise ValueError(f"Missing public checks in {name}: {sorted(selected - found)}")
        for case in source_cases:
            if case["id"] not in selected:
                continue
            ids = case["allowed_source_ids"]
            chunks = []
            for chunk_id in ids:
                source = corpus[chunk_id]
                if source["scope"] != "public" or source["source_kind"] not in {"avito_public", "vendor_docs"}:
                    raise ValueError(f"Expected public evidence: {chunk_id}")
                chunks.append({key: source[key] for key in ("chunk_id", "title", "text")})
            cases.append({
                "id": case["id"],
                "category": "grounded",
                "input": {"question": case["question"], "chunks": chunks, "catalog": []},
                "expect": {"source_ids": ids, "require_source": True, "clarification": False, "draft": None},
                "review_facts": case["expected_facts"],
            })
    return cases


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--corpus", type=Path, default=ROOT / "data/processed/corpus.jsonl")
    parser.add_argument("--output", type=Path, default=ROOT / "data/processed/public-answer-cases.json")
    args = parser.parse_args()
    cases = prepare(args.corpus)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(cases, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Prepared {len(cases)} public answer checks")


if __name__ == "__main__":
    main()
