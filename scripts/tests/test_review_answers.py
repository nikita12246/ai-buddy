import unittest

from scripts.ingest.review_answers import build_packet


class ReviewPacketTest(unittest.TestCase):
    def setUp(self):
        self.case = {
            "id": "grounded-01",
            "category": "grounded",
            "question": "Какая роль нужна?",
            "expected_action": "answer",
            "actor": {"team_id": "atlas", "role": "intern"},
            "expected_facts": ["Роль Reporter"],
            "allowed_source_ids": ["atlas-repo-r1"],
            "forbidden_source_ids": ["orbit-repo-r1"],
        }
        self.policies = [
            {
                "chunk_id": "atlas-repo-r1", "text": "Источник: нужна роль Reporter.",
                "version": "1", "source_kind": "demo_policy", "scope": "team",
                "team_id": "atlas", "allowed_roles": ["intern"],
            },
            {"chunk_id": "orbit-repo-r1", "text": "Чужой источник не показывать."},
        ]
        self.result = {
            "id": self.case["id"],
            "category": self.case["category"],
            "passed": True,
            "review": {
                "expected_facts": self.case["expected_facts"],
                "answer": "Нужна роль Guest.",
                "source_ids": ["atlas-repo-r1"],
                "clarifying_question": None,
            },
        }

    def render(self, *results):
        return build_packet(
            [self.case], self.policies,
            {"passed": sum(item["passed"] for item in results), "results": list(results)},
        )

    def test_automatic_pass_leaves_fact_unchecked(self):
        packet = self.render(self.result)
        self.assertIn("**Автоматический результат:** пройдено.", packet)
        self.assertIn("- [ ] Роль Reporter", packet)
        self.assertNotIn("- [x]", packet)
        self.assertIn("Нужна роль Guest.", packet)
        self.assertIn("Источник: нужна роль Reporter.", packet)
        self.assertIn("**Запрещённые ID:** orbit-repo-r1", packet)
        self.assertNotIn("Чужой источник не показывать.", packet)

    def test_absent_report_and_missing_result(self):
        self.assertIn("Отчёт прогона не приложен", build_packet([self.case], self.policies))
        self.assertIn("результат по случаю отсутствует", self.render())

    def test_result_without_review_shows_failure_separately(self):
        result = dict(self.result, review=None, passed=False, failures=["wrong source"])
        packet = self.render(result)
        self.assertIn("**Автоматический результат:** не пройдено.", packet)
        self.assertIn("wrong source", packet)
        self.assertIn("данных review нет", packet)

    def test_stale_facts_rejected(self):
        review = dict(self.result["review"], expected_facts=["Роль Guest"])
        with self.assertRaisesRegex(ValueError, "stale expected_facts"):
            self.render(dict(self.result, review=review))

    def test_review_rejects_evidence_outside_actor_scope(self):
        self.case["actor"]["team_id"] = "orbit"
        with self.assertRaisesRegex(ValueError, "outside the actor's access"):
            self.render(self.result)

    def test_duplicate_and_unknown_report_ids_rejected(self):
        with self.assertRaisesRegex(ValueError, "duplicate report case ID"):
            self.render(self.result, self.result)
        with self.assertRaisesRegex(ValueError, "unknown report case ID"):
            self.render(dict(self.result, id="unknown"))

    def test_security_context_separates_irrelevant_citation_from_forbidden_source(self):
        case = dict(self.case, id="security-01", category="security", expected_action="deny",
                    allowed_source_ids=[], probe_context_ids=["atlas-repo-r1"])
        review = dict(self.result["review"], source_ids=["atlas-repo-r1", "orbit-repo-r1", "unknown"])
        result = dict(self.result, id=case["id"], category=case["category"], review=review, passed=False)
        packet = build_packet([self.case, case], self.policies, {"passed": 0, "results": [result]},
                              with_security_context=True)
        self.assertNotIn("## grounded-01", packet)
        self.assertIn("Источник: нужна роль Reporter.", packet)
        self.assertNotIn("Чужой источник не показывать.", packet)
        self.assertIn("Цитаты из своего контекста вне эталона: atlas-repo-r1", packet)
        self.assertIn("Запрещённые цитаты: orbit-repo-r1", packet)
        self.assertIn("Прочие цитаты вне контекста: unknown", packet)


if __name__ == "__main__":
    unittest.main()
