"""Prepare AI runner cases with permitted context and restricted requests."""
import argparse
import json
from pathlib import Path

from .checklist import ROOT


def build_probes(cases, policies, resources):
    by_id = {}
    for chunk in policies:
        chunk_id = chunk["chunk_id"]
        if chunk_id in by_id:
            raise ValueError(f"Duplicate policy chunk: {chunk_id}")
        by_id[chunk_id] = chunk

    probes = []
    for case in cases:
        if case["category"] != "security":
            continue
        case_id = case["id"]
        actor = case["actor"]
        context_ids = case.get("probe_context_ids", [])
        if (case["expected_action"] != "deny" or case["allowed_source_ids"]
                or not context_ids or len(context_ids) != len(set(context_ids))):
            raise ValueError(f"{case_id}: probe requires unique context and expects no citations")

        chunks = []
        for chunk_id in context_ids:
            chunk = by_id.get(chunk_id)
            if (not chunk or chunk_id in case["forbidden_source_ids"]
                    or chunk["source_kind"] != "demo_policy" or chunk["scope"] != "team"
                    or chunk["team_id"] != actor["team_id"] or actor["role"] not in chunk["allowed_roles"]):
                raise ValueError(f"{case_id}: invalid context chunk {chunk_id}")
            chunks.append({key: chunk[key] for key in ("chunk_id", "title", "text")})

        catalog = [
            {"tool_id": item["tool_id"], "resource_id": item["resource_id"], "name": item["title"]}
            for item in resources
            if item["team_id"] == actor["team_id"]
        ]
        probes.append({
            "id": case_id,
            "category": "security",
            "input": {"question": case["question"], "chunks": chunks, "catalog": catalog},
            "expect": {"source_ids": [], "clarification": False, "draft": None},
            "review_facts": case["expected_facts"],
        })

    if not probes or len({case["id"] for case in probes}) != len(probes):
        raise ValueError("Expected security probes with unique case IDs")
    return probes


def read_inputs(root):
    cases = json.loads((root / "evals/data-cases.json").read_text(encoding="utf-8"))
    resources = json.loads((root / "data/synthetic/resources.json").read_text(encoding="utf-8"))
    with (root / "data/synthetic/policies.jsonl").open(encoding="utf-8") as source:
        policies = [json.loads(line) for line in source if line.strip()]
    return cases, policies, resources


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / "data/processed/security-with-context.json")
    args = parser.parse_args()
    try:
        probes = build_probes(*read_inputs(ROOT))
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(probes, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    except (OSError, ValueError, KeyError, TypeError) as exc:
        parser.exit(1, f"security probes: {exc}\n")
    print(f"Prepared {len(probes)} security probes with permitted context.")


if __name__ == "__main__":
    main()
