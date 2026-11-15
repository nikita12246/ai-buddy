import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location(
    "check_repo", Path(__file__).resolve().parents[1] / "check_repo.py"
)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class SecretChecks(unittest.TestCase):
    def test_token_is_detected(self):
        self.assertTrue(module.suspicious("ghp_" + "a" * 36))

    def test_key_is_detected(self):
        self.assertTrue(module.suspicious("sk-proj-" + "a" * 30))

    def test_empty_environment_example_is_allowed(self):
        self.assertFalse(module.suspicious("LLM_API_KEY=\nLLM_MODEL=gpt-6.1-sol"))


if __name__ == "__main__":
    unittest.main()
