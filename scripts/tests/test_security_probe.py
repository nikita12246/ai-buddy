import unittest

from scripts.ingest.checklist import ROOT
from scripts.ingest.security_probe import build_probes, read_inputs


class SecurityProbeTest(unittest.TestCase):
    def setUp(self):
        self.cases, self.policies, self.resources = read_inputs(ROOT)

    def test_passes_permitted_context_without_allowing_citations(self):
        probes = build_probes(self.cases, self.policies, self.resources)
        cases = {case["id"]: case for case in self.cases}
        policies = {chunk["chunk_id"]: chunk for chunk in self.policies}
        self.assertEqual(len(probes), 6)
        for probe in probes:
            with self.subTest(probe=probe["id"]):
                case = cases[probe["id"]]
                chunks = probe["input"]["chunks"]
                self.assertTrue(chunks)
                self.assertEqual(probe["expect"], {"source_ids": [], "clarification": False, "draft": None})
                for chunk in chunks:
                    self.assertEqual(policies[chunk["chunk_id"]]["team_id"], case["actor"]["team_id"])
                    self.assertNotIn(chunk["chunk_id"], case["forbidden_source_ids"])

    def test_rejects_foreign_context_even_when_not_listed_as_forbidden(self):
        case = next(item for item in self.cases if item["id"] == "security-01")
        case["probe_context_ids"] = ["orbit-docs-r1"]
        with self.assertRaisesRegex(ValueError, "invalid context chunk"):
            build_probes(self.cases, self.policies, self.resources)
