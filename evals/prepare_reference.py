"""Build an AI-only evaluation input from the data team's synthetic fixtures."""

import argparse
import json
from pathlib import Path


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def prepare(root):
    cases = read_json(root / "evals/data-cases.json")
    policies = {}
    for line in (root / "data/synthetic/policies.jsonl").read_text(encoding="utf-8-sig").splitlines():
        if not line.strip():
            continue
        chunk = json.loads(line)
        if chunk["scope"] != "team" or chunk["source_kind"] != "demo_policy":
            raise ValueError("reference evaluation accepts synthetic team policies only")
        chunk_id = chunk["chunk_id"]
        if chunk_id in policies:
            raise ValueError(f"duplicate policy ID {chunk_id}")
        policies[chunk_id] = chunk

    resources = read_json(root / "data/synthetic/resources.json")
    result = []
    for case in cases:
        actor = case["actor"]
        allowed = case["allowed_source_ids"]
        action = case["expected_action"]
        if action not in {"answer", "clarify", "no_answer", "deny"}:
            raise ValueError(f"{case['id']}: unsupported expected action")
        if set(allowed).intersection(case["forbidden_source_ids"]):
            raise ValueError(f"{case['id']}: source listed as both allowed and forbidden")

        chunks = []
        for chunk_id in allowed:
            chunk = policies[chunk_id]
            if actor["team_id"] != chunk["team_id"] or actor["role"] not in chunk["allowed_roles"]:
                raise ValueError(f"{case['id']}: source {chunk_id} is outside the actor's access")
            chunks.append({key: chunk[key] for key in ("chunk_id", "title", "text")})

        catalog = [
            {"tool_id": resource["tool_id"], "resource_id": resource["resource_id"], "name": resource["title"]}
            for resource in resources
            if resource["team_id"] == actor["team_id"]
        ]
        result.append({
            "id": case["id"],
            "category": case["category"],
            "input": {"question": case["question"], "chunks": chunks, "catalog": catalog},
            "expect": {
                "source_ids": allowed,
                "source_match": "allowed",
                "require_source": action == "answer",
                "clarification": action == "clarify",
                "draft": None,
            },
            "review_facts": case["expected_facts"],
        })
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--data-root", type=Path, default=Path("."))
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    cases = prepare(args.data_root)
    args.output.write_text(json.dumps(cases, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Prepared {len(cases)} reference evaluation cases")


if __name__ == "__main__":
    main()
