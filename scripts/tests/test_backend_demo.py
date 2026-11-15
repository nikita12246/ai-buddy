from contextlib import redirect_stderr, redirect_stdout
import io
import os
from pathlib import Path
import sys
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

from scripts.dev import backend_demo


class DemoModelModeTest(unittest.TestCase):
    def test_live_configuration_loads_dotenv_and_prefers_exported_values(self):
        with TemporaryDirectory() as folder:
            path = Path(folder) / ".env"
            path.write_text('LLM_API_KEY="fixture-model-key"\nexport LLM_MODEL=fixture-model # note\n', encoding="utf-8")
            with patch.dict(os.environ, {"LLM_MODEL": "override-model"}, clear=True):
                values = backend_demo.model_environment(path)
        self.assertEqual(values, {"LLM_API_KEY": "fixture-model-key", "LLM_MODEL": "override-model"})

    def test_smoke_clears_inherited_provider_key(self):
        credentials = {name: "local-demo-value" for name in backend_demo.CREDENTIAL_KEYS}
        with (
            patch.object(sys, "argv", ["backend_demo.py", "up", "--smoke"]),
            patch.dict(os.environ, {"LLM_API_KEY": "fixture-model-key"}, clear=True),
            patch.object(backend_demo, "credentials", return_value=credentials),
            patch.object(backend_demo, "seed", return_value=(b"demo seed", 1)),
            patch.object(backend_demo, "compose") as compose,
            patch.object(backend_demo, "initialize"),
            patch.object(backend_demo, "smoke") as smoke,
            redirect_stdout(io.StringIO()),
        ):
            backend_demo.main()

        self.assertEqual(compose.call_count, 3)
        for call in compose.call_args_list:
            self.assertEqual(call.args[0]["LLM_API_KEY"], "")
        smoke.assert_called_once()

    def test_smoke_rejects_live_mode_before_launch(self):
        with (
            patch.object(sys, "argv", ["backend_demo.py", "up", "--smoke", "--live"]),
            patch.dict(os.environ, {"LLM_API_KEY": "fixture-model-key"}, clear=True),
            patch.object(backend_demo, "compose") as compose,
            redirect_stderr(io.StringIO()),
            self.assertRaises(SystemExit) as exit_context,
        ):
            backend_demo.main()

        self.assertEqual(exit_context.exception.code, 2)
        compose.assert_not_called()
