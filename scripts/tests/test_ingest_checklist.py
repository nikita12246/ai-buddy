import json
import tempfile
import unittest
from pathlib import Path

from scripts.ingest.checklist import import_checklist


class ChecklistImportTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.output = self.root / "processed/checklist.jsonl"
        (self.root / "general.csv").write_text(
            '"Материалы\n чек-листа",Описание пункта,Период прохождения задачи\n'
            ',,\n'
            'Запроси доступ,"Проверь Jira,\nзатем Confluence",1 неделя\n',
            encoding="utf-8",
            newline="\n",
        )
        (self.root / "backend.csv").write_text(
            "Материалы чек-листа,Период прохождения задачи\nНастрой инструмент,1 месяц\n",
            encoding="utf-8",
        )

    def test_preserves_row_locator_multiline_text_and_period(self):
        self.assertEqual(import_checklist(self.root, self.output), 2)
        general, backend = [
            json.loads(line) for line in self.output.read_text(encoding="utf-8").splitlines()
        ]
        self.assertEqual(general["chunk_id"], "avito-checklist-general-r3")
        self.assertTrue(general["source_url"].endswith("#gid=1073060303&range=A3:C3"))
        self.assertIn("Проверь Jira,\nзатем Confluence", general["text"])
        self.assertEqual(general["period"], "1 неделя")
        self.assertTrue(backend["source_url"].endswith("#gid=0&range=A2:B2"))
        self.assertNotEqual(general["version"], backend["version"])

    def test_invalid_second_tab_leaves_previous_import_intact(self):
        import_checklist(self.root, self.output)
        previous = self.output.read_bytes()
        (self.root / "backend.csv").write_text("Unexpected,header\n", encoding="utf-8")

        with self.assertRaisesRegex(ValueError, "unexpected columns"):
            import_checklist(self.root, self.output)

        self.assertEqual(self.output.read_bytes(), previous)
