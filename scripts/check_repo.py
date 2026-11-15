"""Small repository checks; not a replacement for a dedicated secret scanner."""
import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PATTERNS = [
    re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
    re.compile(r"\bgh[pousr]_[A-Za-z0-9]{30,}\b"),
    re.compile(r"\bgithub_pat_[A-Za-z0-9_]{40,}\b"),
    re.compile(r"\bsk-(?:proj-|ant-)?[A-Za-z0-9_-]{24,}\b"),
]


def suspicious(text):
    return any(pattern.search(text) for pattern in PATTERNS)


def main():
    files = subprocess.check_output(
        ["git", "ls-files", "-z"], cwd=ROOT
    ).decode("utf-8").split("\0")
    errors = []
    for name in filter(None, files):
        path = ROOT / name
        if path.name.startswith(".env") and path.name != ".env.example":
            errors.append(f"Environment file tracked: {name}")
        if path.suffix in {".pem", ".key", ".p12", ".pfx"}:
            errors.append(f"Credential-like file tracked: {name}")
        try:
            text = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        if suspicious(text):
            errors.append(f"Potential secret in {name}; value suppressed")
    sources = json.loads((ROOT / "data/sources.json").read_text(encoding="utf-8"))
    ids = [source["id"] for source in sources]
    if len(ids) != len(set(ids)):
        errors.append("Duplicate source IDs")
    if errors:
        raise SystemExit("\n".join(errors))
    print("Repository checks passed (tracked files only).")


if __name__ == "__main__":
    main()
