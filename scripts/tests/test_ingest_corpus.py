import tempfile
import unittest
from pathlib import Path

from scripts.ingest.corpus import public_chunks


class PublicDocumentTest(unittest.TestCase):
    def test_markdown_keeps_source_lines(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "page.md").write_text("# Contents\n- [Heading](#heading)\n\n# Heading\n\nFirst paragraph.\n", encoding="utf-8")
            entry = {
                "id": "test-doc", "source_id": "test", "kind": "markdown",
                "title": "Test", "path": "page.md", "url": "https://example.com/page.md",
            }
            chunk, = public_chunks(root, entry)
            self.assertEqual(chunk["title"], "Heading")
            self.assertEqual(chunk["chunk_id"], "test-doc-s2")
            self.assertEqual(chunk["source_url"], "https://example.com/page.md#L6")

    def test_html_ignores_navigation_and_scripts(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "page.html").write_text(
                "<header>Outside</header><article><nav>Menu</nav><p>Useful &amp; safe</p>"
                "<script>ignored()</script><p>Next step</p></article><footer>Outside</footer>",
                encoding="utf-8",
            )
            entry = {
                "id": "test-doc", "source_id": "test", "kind": "html",
                "title": "Test", "path": "page.html", "url": "https://example.com/page",
            }
            chunk, = public_chunks(root, entry)
            self.assertEqual(chunk["text"], "Useful & safe\nNext step")

    def test_html_selects_requested_section_from_nested_container(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "page.html").write_text(
                '<main><h3>Outside</h3><div class="body article-body">'
                '<h3>Ignore me</h3><p>Other section.</p><h3>First <em>days</em></h3>'
                '<div><p>First paragraph.</p></div><p>Second paragraph.</p>'
                '</div><h3>Recommendations</h3><p>Outside body.</p></main>',
                encoding="utf-8",
            )
            entry = {
                "id": "test-doc", "source_id": "test", "kind": "html",
                "title": "Article (2021)", "path": "page.html", "url": "https://example.com/page",
                "content_class": "article-body", "headings": ["First days"],
            }
            chunk, = public_chunks(root, entry)
            self.assertEqual(chunk["chunk_id"], "test-doc-s2")
            self.assertEqual(chunk["title"], "Article (2021) — First days")
            self.assertEqual(chunk["text"], "First paragraph.\nSecond paragraph.")
            self.assertEqual(chunk["source_url"], "https://example.com/page")
            with self.assertRaisesRegex(ValueError, "Missing sections"):
                list(public_chunks(root, dict(entry, headings=["Missing"])))

    def test_summary_uses_manifest_directory_and_links_to_original(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "notes.md").write_text("# A summary\n\nOur own summary.\n", encoding="utf-8")
            entry = {
                "id": "test-doc", "source_id": "test", "kind": "summary",
                "title": "Title", "path": "notes.md", "url": "https://example.com/original",
            }
            chunk, = public_chunks(root / "unused", entry, root)
            self.assertEqual(chunk["text"], "Our own summary.")
            self.assertEqual(chunk["source_url"], "https://example.com/original")
