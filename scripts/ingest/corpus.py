"""Build a versioned knowledge corpus from local source snapshots."""
import argparse
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path
import re

from .checklist import ROOT, TABS, read_tab, write_jsonl

# Markdown tables of contents list only links to sections of the same document.
MARKDOWN_NAVIGATION = re.compile(r"(?m)^[ \t]*[-+*][ \t]+\[[^\]\n]+\]\(#[^)\n]+\)[ \t]*$")


class ArticleText(HTMLParser):
    """Read a selected article container and retain its section headings."""

    headings = {"h1", "h2", "h3", "h4", "h5", "h6"}
    blocks = {"p", "li", "br", "blockquote", "pre"} | headings
    skipped = {"script", "style", "nav", "aside", "noscript"}

    def __init__(self, content_class=None):
        super().__init__()
        self.content_class = content_class
        self.container = None
        self.container_depth = 0
        self.skip = None
        self.skip_depth = 0
        self.complete = False
        self.parts = []

    def handle_starttag(self, tag, attrs):
        if self.complete:
            return
        if self.container is None:
            if self.content_class:
                matches = self.content_class in (dict(attrs).get("class") or "").split()
            else:
                matches = tag in {"article", "main"}
            if matches:
                self.container = tag
                self.container_depth = 1
            return

        if tag == self.container:
            self.container_depth += 1
        if self.skip is not None:
            if tag == self.skip:
                self.skip_depth += 1
        elif tag in self.skipped:
            self.skip = tag
            self.skip_depth = 1
        elif tag in self.headings:
            self.parts.append("\n### ")
        elif tag in self.blocks:
            self.parts.append("\n")

    def handle_endtag(self, tag):
        if tag == self.container:
            self.container_depth -= 1
            if self.container_depth == 0:
                self.complete = True
                self.container = None
                return

        if self.skip is not None:
            if tag == self.skip:
                self.skip_depth -= 1
                if self.skip_depth == 0:
                    self.skip = None
        elif self.container is not None and tag in self.blocks:
            self.parts.append("\n")

    def handle_data(self, data):
        if self.container is not None and self.skip is None:
            self.parts.append(data)

    def text(self):
        return "\n".join(
            line for part in "".join(self.parts).splitlines()
            if (line := " ".join(part.split()))
        )


def markdown_sections(text, title):
    heading = title
    lines = []
    for number, line in enumerate(text.splitlines(), 1):
        match = re.fullmatch(r"#{1,6}\s+(.+?)\s*#*", line)
        if match:
            body = "\n".join(lines).strip()
            if body:
                yield start, heading, body
            heading = match.group(1)
            lines = []
        elif lines or line.strip():
            if not lines:
                start = number
            lines.append(line)
    if (body := "\n".join(lines).strip()):
        yield start, heading, body


def public_chunks(raw_dir, entry, manifest_dir=None):
    kind = entry["kind"]
    source_kind = entry.get("source_kind", "avito_public")
    if source_kind not in {"avito_public", "vendor_docs"}:
        raise ValueError(f"Invalid public source kind: {source_kind}")
    source_dir = manifest_dir if kind == "summary" else raw_dir
    if source_dir is None:
        raise ValueError("Summary import needs the manifest directory")
    raw = (source_dir / entry["path"]).read_bytes()
    text = raw.decode("utf-8-sig")
    version = "sha256:" + hashlib.sha256(raw).hexdigest()
    if kind in {"markdown", "summary"}:
        sections = [
            (line, title, MARKDOWN_NAVIGATION.sub("", body).strip())
            for line, title, body in markdown_sections(text, entry["title"])
        ]
    elif kind == "html":
        parser = ArticleText(entry.get("content_class"))
        parser.feed(text)
        parser.close()
        sections = [
            (None, title, body)
            for _, title, body in markdown_sections(parser.text(), entry["title"])
        ]
    else:
        raise ValueError(f"Unsupported document type: {entry['kind']}")
    if not sections or not any(section[2] for section in sections):
        raise ValueError(f"No readable content in {entry['path']}")
    selected_headings = set(entry.get("headings", []))
    missing = selected_headings - {title for _, title, body in sections if body}
    if missing:
        raise ValueError(f"Missing sections in {entry['path']}: {', '.join(sorted(missing))}")
    for index, (line, title, body) in enumerate(sections, 1):
        if not body or (selected_headings and title not in selected_headings):
            continue
        if kind == "html" and title != entry["title"]:
            title = f"{entry['title']} — {title}"
        yield {
            "chunk_id": f"{entry['id']}-s{index}",
            "document_id": entry["id"],
            "title": title,
            "text": body,
            "source_id": entry["source_id"],
            "source_url": entry["url"] + (f"#L{line}" if kind == "markdown" and line else ""),
            "version": version,
            "scope": "public",
            "source_kind": source_kind,
        }


def build_corpus(raw_dir, manifest, policies, output):
    chunks = [chunk for tab in TABS for chunk in read_tab(raw_dir / "avito-checklist", *tab)]
    for entry in json.loads(manifest.read_text(encoding="utf-8")):
        chunks.extend(public_chunks(raw_dir, entry, manifest.parent))
    with policies.open(encoding="utf-8") as file:
        chunks.extend(json.loads(line) for line in file if line.strip())
    if len({chunk["chunk_id"] for chunk in chunks}) != len(chunks):
        raise ValueError("Duplicate chunk ID in corpus")
    write_jsonl(output, chunks)
    return len(chunks)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--raw-dir", type=Path, default=ROOT / "data/raw")
    parser.add_argument("--manifest", type=Path, default=ROOT / "data/public-documents.json")
    parser.add_argument("--policies", type=Path, default=ROOT / "data/synthetic/policies.jsonl")
    parser.add_argument("--output", type=Path, default=ROOT / "data/processed/corpus.jsonl")
    args = parser.parse_args()
    count = build_corpus(args.raw_dir, args.manifest, args.policies, args.output)
    print(f"Built {count} chunks in {args.output}")


if __name__ == "__main__":
    main()
