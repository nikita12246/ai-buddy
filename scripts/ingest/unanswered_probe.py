"""Build AI cases from the actual top-three search results for unanswered questions."""
import argparse
import json
from pathlib import Path

from .checklist import ROOT


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def prepare(corpus_path, context_path):
    cases = read_json(ROOT / "evals/unanswered-cases.json")
    resources = read_json(ROOT / "data/synthetic/resources.json")
    context = read_json(context_path)
    by_case = {item["id"]: item["chunk_ids"] for item in context}
    if len(by_case) != len(context) or set(by_case) != {case["id"] for case in cases}:
        raise ValueError("Search context does not match the unanswered cases")
    with corpus_path.open(encoding="utf-8") as file:
        items = (json.loads(line) for line in file if line.strip())
        chunks = {item["chunk_id"]: item for item in items}

    result = []
    for case in cases:
        case_id = case["id"]
        actor = case["actor"]
        ids = by_case[case_id]
        if not ids or len(ids) > 3 or len(ids) != len(set(ids)):
            raise ValueError(f"{case_id}: expected one to three distinct search results")
        if not set(ids).intersection(case["context_source_ids"]):
            raise ValueError(f"{case_id}: missing a related search result")
        evidence = []
        for chunk_id in ids:
            chunk = chunks[chunk_id]
            allowed = chunk["scope"] == "public" or (
                chunk["scope"] == "team" and chunk["team_id"] == actor["team_id"]
                and actor["role"] in chunk["allowed_roles"]
            )
            if not allowed:
                raise ValueError(f"{case_id}: search result is outside the actor's access: {chunk_id}")
            evidence.append({key: chunk[key] for key in ("chunk_id", "title", "text")})
        catalog = [
            {"tool_id": resource["tool_id"], "resource_id": resource["resource_id"], "name": resource["title"]}
            for resource in resources if resource["team_id"] == actor["team_id"]
        ]
        result.append({
            "id": case_id,
            "category": "no_answer",
            "input": {"question": case["question"], "chunks": evidence, "catalog": catalog},
            "expect": {"source_ids": ids, "source_match": "allowed", "clarification": False, "draft": None},
            "review_facts": case["review_facts"],
        })
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    processed = ROOT / "data/processed"
    parser.add_argument("--corpus", type=Path, default=processed / "corpus.jsonl")
    parser.add_argument("--context", type=Path, default=processed / "unanswered-context.json")
    parser.add_argument("--output", type=Path, default=processed / "unanswered-with-context.json")
    args = parser.parse_args()
    cases = prepare(args.corpus, args.context)
    args.output.write_text(json.dumps(cases, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Prepared {len(cases)} checks with the actual search context")


if __name__ == "__main__":
    main()
