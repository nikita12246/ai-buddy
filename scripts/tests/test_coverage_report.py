import unittest

from scripts.coverage_report import read_go, read_lcov, render


class CoverageReportTest(unittest.TestCase):
    def test_go_blocks_merge_line_hits_and_use_an_exclusive_end(self):
        lines = read_go(
            "mode: atomic\n"
            "backend.go:2.1,4.1 2 1\n"
            "backend.go:3.2,5.3 1 0\n"
            "backend.go:6.1,6.3 0 1\n"
        )
        self.assertEqual(lines, {
            ("backend.go", 2): True, ("backend.go", 3): True,
            ("backend.go", 4): False, ("backend.go", 5): False,
        })

    def test_lcov_merges_repeated_records(self):
        lines = read_lcov(
            "SF:app.ts\nDA:2,0\nDA:3,1,checksum\nend_of_record\n"
            "SF:app.ts\nDA:2,1\nend_of_record\n"
        )
        self.assertEqual(lines, {("app.ts", 2): True, ("app.ts", 3): True})

    def test_total_is_weighted_and_threshold_is_strict(self):
        frontend = {("app.ts", i): i < 3 for i in range(4)}
        report = render({("api.go", 1): True}, frontend)
        self.assertIn("| Всего | 4 | 5 | 80.00% |", report)
        self.assertIn("пока не достигнут", report)
        with self.assertRaises(ValueError):
            render({}, frontend)
