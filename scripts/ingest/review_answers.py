"""Build an offline Markdown packet for manual AI answer review."""

import argparse
import json
from pathlib import Path
import re


def index_by_id(items, key, label):
    indexed = {}
    for item in items:
        item_id = item[key]
        if item_id in indexed:
            raise ValueError(f"duplicate {label} ID: {item_id}")
        indexed[item_id] = item
    return indexed


def report_results(report, cases):
    if report is None:
        return {}
    results = index_by_id(report["results"], "id", "report case")
    for case_id, result in results.items():
        if case_id not in cases:
            raise ValueError(f"unknown report case ID: {case_id}")
        if type(result["passed"]) is not bool:
            raise ValueError(f"invalid automatic outcome for {case_id}")
        case = cases[case_id]
        if result["category"] != case["category"]:
            raise ValueError(f"stale category for {case_id}")
        review = result.get("review")
        if review is not None and review.get("expected_facts") != case["expected_facts"]:
            raise ValueError(f"stale expected_facts for {case_id}")
    if report["passed"] != sum(item["passed"] for item in results.values()):
        raise ValueError("report passed total does not match individual results")
    return results


def text_block(value):
    """Render untrusted text literally, including Markdown fences and HTML."""
    if value is None or value == "":
        return "—"
    backticks = max((len(run) for run in re.findall(r"`+", value)), default=0)
    fence = "`" * max(3, backticks + 1)
    return f"{fence}text\n{value}\n{fence}"


def format_ids(ids):
    return ", ".join(ids or []) or "—"


def build_packet(golden_cases, policies, report=None, *, with_security_context=False):
    if with_security_context:
        golden_cases = [case for case in golden_cases if case["category"] == "security"]
        if not golden_cases:
            raise ValueError("no security cases to review")
    cases = index_by_id(golden_cases, "id", "golden case")
    evidence = index_by_id(policies, "chunk_id", "policy")
    results = report_results(report, cases)
    sections = [
        "# Ручная проверка ответов AI Buddy",
        "Отметьте каждый факт после сверки ответа с допустимыми источниками. "
        "Автоматический результат показан отдельно. Смысловое соответствие оценивает проверяющий.",
    ]
    if report is None:
        sections.append("Отчёт прогона не приложен.")
    else:
        metadata = (
            f"Провайдер: {report.get('provider') or '—'}\n"
            f"Модель: {report.get('model') or '—'}\n"
            f"Версия промпта: {report.get('prompt_version') or '—'}\n"
            f"Автоматически пройдено: {report['passed']} из {len(results)}"
        )
        sections.append(text_block(metadata))
    for case_id, case in cases.items():
        allowed = case["allowed_source_ids"]
        forbidden = case["forbidden_source_ids"]
        context = case["probe_context_ids"] if with_security_context else allowed
        if with_security_context and (allowed or not context or len(context) != len(set(context))):
            raise ValueError(f"{case_id}: expected unique context and no permitted citations")
        sections.extend([
            f"## {case_id} · {case['category']}",
            f"**Вопрос**\n\n{text_block(case['question'])}",
            f"**Ожидаемое действие:** {case['expected_action']}",
            "**Факты для ручной проверки**",
            "\n".join(f"- [ ] {fact}" for fact in case["expected_facts"]) or "—",
            f"**Допустимые ID:** {format_ids(allowed)}\n\n"
            f"**Запрещённые ID:** {format_ids(forbidden)}",
        ])
        if with_security_context:
            sections.append(
                "**Контекст сценария для модели**\n\n"
                "Фрагменты своей команды передаются как фон. Эталон отказа ожидает ответ без ссылок."
            )
        else:
            sections.append("**Ожидаемые материалы из синтетического корпуса**")
            if not context:
                sections.append("Допустимых источников нет.")
        for source_id in context:
            if source_id in forbidden:
                raise ValueError(f"source {source_id} both allowed and forbidden for {case_id}")
            if source_id not in evidence:
                raise ValueError(f"unknown allowed policy ID for {case_id}: {source_id}")
            policy = evidence[source_id]
            actor = case["actor"]
            if (policy["source_kind"] != "demo_policy" or policy["scope"] != "team"
                    or actor["team_id"] != policy["team_id"]
                    or actor["role"] not in policy["allowed_roles"]):
                raise ValueError(f"{case_id}: source {source_id} is outside the actor's access")
            sections.append(
                f"**{source_id} · версия {policy['version']}**\n\n{text_block(policy['text'])}"
            )

        sections.append(
            "- [ ] Действие соответствует эталону.\n"
            "- [ ] Ответ не добавляет неподтверждённых утверждений.\n"
            "- [ ] Источники и права соблюдены."
        )
        result = results.get(case_id)
        if result is None:
            sections.append("**Автоматический результат:** результат по случаю отсутствует.")
        else:
            outcome = "пройдено" if result["passed"] else "не пройдено"
            sections.append(f"**Автоматический результат:** {outcome}.")
            if "duration_ms" in result:
                usage = "есть" if result.get("usage") else "нет"
                sections.append(f"Длительность — {result['duration_ms']} мс. Данные о токенах — {usage}.")
            if result.get("failures"):
                sections.append(text_block("\n".join(result["failures"])))
        review = result.get("review") if result else None
        if review is None:
            sections.append("**Ответ для ручной проверки:** данных review нет.")
        else:
            sections.extend([
                f"**Фактический ответ**\n\n{text_block(review.get('answer'))}",
                f"**Цитированные ID**\n\n{text_block(format_ids(review.get('source_ids')))}",
                "**Уточняющий вопрос**\n\n"
                f"{text_block(review.get('clarifying_question'))}",
            ])
            if with_security_context:
                cited = set(review.get("source_ids") or [])
                sections.append(text_block(
                    f"Цитаты из своего контекста вне эталона: {format_ids(sorted(cited.intersection(context)))}\n"
                    f"Запрещённые цитаты: {format_ids(sorted(cited.intersection(forbidden)))}\n"
                    f"Прочие цитаты вне контекста: {format_ids(sorted(cited.difference(context, forbidden)))}"
                ))
    return "\n\n".join(sections) + "\n"


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", type=Path, help="optional JSON report from ai-eval -review")
    parser.add_argument("--with-security-context", action="store_true",
                        help="review the separate security-with-context run")
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--output", type=Path, help="defaults to a review file in data/processed")
    args = parser.parse_args(argv)
    filename = "security-context-review.md" if args.with_security_context else "ai-review.md"
    output = args.output or args.repo_root / "data/processed" / filename
    try:
        cases = read_json(args.repo_root / "evals/data-cases.json")
        policy_path = args.repo_root / "data/synthetic/policies.jsonl"
        with policy_path.open(encoding="utf-8") as handle:
            policies = [json.loads(line) for line in handle if line.strip()]
        report = read_json(args.report) if args.report else None
        packet = build_packet(cases, policies, report, with_security_context=args.with_security_context)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(packet, encoding="utf-8")
    except (OSError, ValueError, KeyError, TypeError) as exc:
        parser.exit(1, f"review packet: {exc}\n")


if __name__ == "__main__":
    main()
