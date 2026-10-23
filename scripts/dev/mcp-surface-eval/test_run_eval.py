"""Tests for the runner's safety guards."""

from __future__ import annotations

import os
import pathlib
import tempfile
import unittest
from unittest import mock

import run_eval


class BindingsPathTest(unittest.TestCase):
    """The bindings file holds private question text."""

    def test_refuses_a_path_inside_the_repository(self) -> None:
        inside = run_eval._REPO_ROOT / "scripts" / "bindings.json"
        with self.assertRaises(SystemExit):
            run_eval.check_bindings_path(inside)

    def test_refuses_the_repository_root_itself(self) -> None:
        with self.assertRaises(SystemExit):
            run_eval.check_bindings_path(run_eval._REPO_ROOT)

    def test_allows_a_path_outside_the_repository(self) -> None:
        with tempfile.TemporaryDirectory() as outside:
            run_eval.check_bindings_path(pathlib.Path(outside) / "b.json")


class CodexHomeTest(unittest.TestCase):
    """The isolated Codex home needs the host config."""

    def test_missing_host_config_is_a_clear_exit(self) -> None:
        with tempfile.TemporaryDirectory() as home, \
                tempfile.TemporaryDirectory() as work:
            with mock.patch.object(pathlib.Path, "home",
                                   return_value=pathlib.Path(home)):
                with self.assertRaises(SystemExit) as ctx:
                    run_eval.make_codex_home(pathlib.Path(work))
        self.assertIn("config.toml", str(ctx.exception))

    def test_carries_over_only_the_model_lines(self) -> None:
        with tempfile.TemporaryDirectory() as home, \
                tempfile.TemporaryDirectory() as work:
            codex = pathlib.Path(home) / ".codex"
            codex.mkdir()
            (codex / "config.toml").write_text(
                'model = "m"\nother = 1\n[section]\nmodel = "x"\n')
            with mock.patch.object(pathlib.Path, "home",
                                   return_value=pathlib.Path(home)):
                made = run_eval.make_codex_home(pathlib.Path(work))
            text = pathlib.Path(made, "config.toml").read_text()
        self.assertEqual(text, 'model = "m"\n')


class TempDirectoryTest(unittest.TestCase):
    """The runner removes its temporary directory unless asked to keep it."""

    def _run(self, *extra: str) -> list[pathlib.Path]:
        made: list[pathlib.Path] = []
        real = tempfile.mkdtemp

        def tracking(*a: object, **k: object) -> str:
            path = real(*a, **k)
            made.append(pathlib.Path(path))
            return path

        env = {"ESHU_MCP_BASE_URL": "http://h", "ESHU_MCP_KEY": "k"}
        with tempfile.TemporaryDirectory() as out, \
                mock.patch.dict(os.environ, env), \
                mock.patch.object(run_eval, "client_version",
                                  return_value="test"), \
                mock.patch.object(run_eval.tempfile, "mkdtemp", tracking):
            run_eval.main(["--client", "claude", "--out", out,
                           "--only", "none-such", *extra])
        return made

    def test_temp_directory_is_removed_at_exit(self) -> None:
        made = self._run()
        self.assertEqual(len(made), 1)
        self.assertFalse(made[0].exists())

    def test_keep_leaves_the_temp_directory(self) -> None:
        made = self._run("--keep")
        try:
            self.assertTrue(made[0].exists())
        finally:
            import shutil
            shutil.rmtree(made[0], ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
