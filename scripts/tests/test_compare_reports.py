import json
from pathlib import Path
import subprocess
import sys
from tempfile import TemporaryDirectory
import unittest


SCRIPT = Path(__file__).resolve().parents[2] / "evals/compare_reports.py"


class CompareReportsTest(unittest.TestCase):
    def test_report_uses_one_snapshot_and_distinguishes_local_replies(self):
        shared = {"provider": "fake", "prompt_version": "b" * 64,
                  "cases_sha256": "a" * 64, "passed": 2}
        results = [
            {"id": "g1", "category": "grounded", "duration_ms": 202, "passed": True,
             "local_reply": False, "client_error": False,
             "usage": {"input_tokens": 300, "output_tokens": 200}},
            {"id": "s1", "category": "security", "duration_ms": 0, "passed": True,
             "local_reply": True, "client_error": False},
            {"id": "g2", "category": "grounded", "duration_ms": 901, "passed": False,
             "local_reply": False, "client_error": True, "failures": ["provider error"]},
        ]
        with TemporaryDirectory() as folder:
            first, second = (Path(folder) / name for name in ("gpt.json", "sonnet.json"))
            first.write_text(json.dumps({**shared, "model": "gpt", "results": results}))
            second_report = {**shared, "model": "sonnet", "results": results}
            second.write_text(json.dumps(second_report))
            command = [sys.executable, str(SCRIPT), str(first), str(second), "--first-rate", "2,5", "--currency", "USD"]
            success = subprocess.run(command, check=True, capture_output=True, text=True)
            self.assertIn("202 мс", success.stdout)
            self.assertIn("Локальные ответы: 1", success.stdout)
            self.assertIn("Ошибки клиента: 1", success.stdout)
            self.assertIn("0.0016 USD", success.stdout)
            self.assertIn("grounded 1/2, security 1/1", success.stdout)

            second.write_text(json.dumps({**second_report, "cases_sha256": "c" * 64}))
            failure = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(failure.returncode, 0)
            self.assertIn("same evaluation file", failure.stderr)
