"""Combine Go coverage block line ranges with the frontend LCOV line records."""
import argparse
from pathlib import Path
import re


GO_BLOCK = re.compile(r"(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)")


def record(lines, path, line, hits):
    key = (path, line)
    lines[key] = lines.get(key, False) or hits > 0


def read_go(text):
    header, *blocks = text.splitlines()
    if header not in {"mode: set", "mode: count", "mode: atomic"}:
        raise ValueError("Missing Go coverage profile mode")
    lines = {}
    for block in blocks:
        match = GO_BLOCK.fullmatch(block)
        if not match:
            raise ValueError(f"Malformed Go coverage block: {block}")
        path, start, start_column, end, end_column, statements, hits = match.groups()
        start, start_column, end, end_column = map(int, (start, start_column, end, end_column))
        if min(start, start_column, end, end_column) < 1 or (end, end_column) < (start, start_column):
            raise ValueError(f"Invalid Go coverage range: {block}")
        if int(statements) == 0:
            continue
        # The end coordinate is exclusive, including when it is the first column.
        for line in range(start, end + (end_column > 1)):
            record(lines, path, line, int(hits))
    return lines


def read_lcov(text):
    lines = {}
    source = None
    for entry in text.splitlines():
        if entry.startswith("SF:"):
            source = entry[3:]
            if not source:
                raise ValueError("Empty LCOV source path")
        elif entry.startswith("DA:"):
            line, hits, *_ = entry[3:].split(",")
            if source is None or int(line) < 1 or int(hits) < 0:
                raise ValueError(f"Invalid LCOV line: {entry}")
            record(lines, source, int(line), int(hits))
        elif entry == "end_of_record":
            source = None
    return lines


def render(go_lines, frontend_lines):
    if not go_lines or not frontend_lines:
        raise ValueError("Both coverage reports must contain line data")
    go_covered = sum(go_lines.values())
    frontend_covered = sum(frontend_lines.values())
    covered = go_covered + frontend_covered
    total = len(go_lines) + len(frontend_lines)
    rows = [
        ("Backend Go, включая mock", go_covered, len(go_lines)),
        ("Frontend TypeScript и React", frontend_covered, len(frontend_lines)),
        ("Всего", covered, total),
    ]
    table = ["| Код | Покрыто строк | Учтено строк | Покрытие |", "| --- | ---: | ---: | ---: |"]
    table.extend(f"| {name} | {hits} | {count} | {100 * hits / count:.2f}% |" for name, hits, count in rows)
    result = "достигнут" if covered * 100 > total * 80 else "пока не достигнут"
    return "\n".join([
        "## Покрытие строк", "", *table, "",
        f"Порог строго выше 80% {result}.", "",
        "Для Go строки определяются по диапазонам инструментированных блоков с операторами. "
        "Это оценка по блокам, а внутри многострочного блока учитываются все строки его диапазона. "
        "Для frontend используются записи DA из LCOV. Строка считается покрытой при попадании хотя бы в один выполненный блок или запись.",
    ])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("go_profile", type=Path)
    parser.add_argument("frontend_lcov", type=Path)
    args = parser.parse_args()
    try:
        print(render(read_go(args.go_profile.read_text(encoding="utf-8")),
                     read_lcov(args.frontend_lcov.read_text(encoding="utf-8"))))
    except (OSError, ValueError) as error:
        parser.error(str(error))


if __name__ == "__main__":
    main()
