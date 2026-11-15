"""Compare two matching ai-eval runs without calling the provider."""

import argparse
from collections import Counter
from decimal import Decimal, InvalidOperation
import json
from pathlib import Path
import re
from statistics import median


def load_report(path):
    report = json.loads(path.read_text(encoding="utf-8"))
    for key in ("cases_sha256", "prompt_version"):
        if not re.fullmatch(r"[0-9a-f]{64}", report[key]):
            raise ValueError(f"{path.name}: invalid {key}")
    results = report["results"]
    case_ids = {(item["id"], item["category"]) for item in results}
    if not results or len({case_id for case_id, _ in case_ids}) != len(results):
        raise ValueError(f"{path.name}: empty report or duplicate case ID")
    if report["passed"] != sum(item["passed"] for item in results):
        raise ValueError(f"{path.name}: invalid pass total")
    return report, case_ids


def render(report, rate, currency):
    results = report["results"]
    replies = [item for item in results if not item["local_reply"] and not item["client_error"]]
    with_usage = [item["usage"] for item in replies if item.get("usage") is not None]
    inputs = sum(item["input_tokens"] for item in with_usage)
    outputs = sum(item["output_tokens"] for item in with_usage)
    groups = Counter(item["category"] for item in results)
    passed = Counter(item["category"] for item in results if item["passed"])
    failures = Counter(reason for item in results for reason in item.get("failures", []))
    lines = [
        f"## {report['provider']} / {report['model']}",
        "",
        f"- Автоматические проверки: {report['passed']}/{len(results)}.",
        "- По группам: " + ", ".join(f"{name} {passed[name]}/{groups[name]}" for name in sorted(groups)) + ".",
        f"- Ответы модели: {len(replies)}. Локальные ответы: {sum(item['local_reply'] for item in results)}. "
        f"Ошибки клиента: {sum(item['client_error'] for item in results)}.",
        "- Медиана задержки успешных вызовов: "
        + (f"{median(item['duration_ms'] for item in replies):g} мс." if replies else "нет данных."),
        f"- Данные о расходе: {len(with_usage)}/{len(replies)} ответов модели, "
        f"{inputs} входных и {outputs} выходных токенов.",
    ]
    if rate is not None and with_usage:
        cost = (inputs * rate[0] + outputs * rate[1]) / 1_000_000
        lines.append(f"- Стоимость по записанным токенам: {cost:f} {currency}.")
    if failures:
        lines.extend(["", "Автоматические расхождения", ""])
        lines.extend(f"- {reason} — {count}." for reason, count in failures.most_common())
    return "\n".join(lines)


def parse_rate(value):
    try:
        rates = tuple(Decimal(item) for item in value.split(","))
        if len(rates) != 2 or any(not rate.is_finite() or rate < 0 for rate in rates):
            raise ValueError
        return rates
    except (InvalidOperation, ValueError) as exc:
        raise argparse.ArgumentTypeError("expected two nonnegative prices separated by a comma") from exc


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("first", type=Path)
    parser.add_argument("second", type=Path)
    parser.add_argument("--first-rate", type=parse_rate, help="input,output prices per million tokens")
    parser.add_argument("--second-rate", type=parse_rate, help="input,output prices per million tokens")
    parser.add_argument("--currency", help="currency shared by both prices")
    args = parser.parse_args()
    if (args.first_rate or args.second_rate) and not args.currency:
        parser.error("specify --currency when supplying prices")
    try:
        first, first_ids = load_report(args.first)
        second, second_ids = load_report(args.second)
        if first_ids != second_ids or any(first[k] != second[k] for k in ("cases_sha256", "prompt_version")):
            raise ValueError("runs must use the same evaluation file, cases and prompt version")
        print(
            "# Сравнение моделей\n\n"
            f"SHA-256 входного набора — `{first['cases_sha256']}`.\n\n"
            f"Версия промпта — `{first['prompt_version']}`.\n\n"
            + render(first, args.first_rate, args.currency) + "\n\n"
            + render(second, args.second_rate, args.currency)
            + "\n\nСмысловую полноту и обоснованность оцените по карточкам ручной проверки.\n"
        )
    except (OSError, ValueError, KeyError, TypeError) as exc:
        parser.exit(1, f"compare reports: {exc}\n")


if __name__ == "__main__":
    main()
