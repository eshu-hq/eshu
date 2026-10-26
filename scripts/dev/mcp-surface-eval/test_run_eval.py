"""Tests for the runner's safety guards."""

from __future__ import annotations

import json
import os
import pathlib
import tempfile
import unittest
from unittest import mock

import run_eval
import score


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


class ShapeSelectionTest(unittest.TestCase):
    """The requested shape reaches either client's hosted MCP URL."""

    def test_selected_shape_reaches_both_clients_and_metadata(self) -> None:
        task = score.Task("test-id", "code_lookup", "find_symbol", (),
                          frozenset({"find_symbol"}))
        result = score.Score("test-id", "code_lookup", 0, 0, 0, False,
                             False, 0.0)
        run = score.Run([], "", 0.0, 0, False)
        for client in ("claude", "codex"):
            for shape in ("dual", "structured", None):
                with self.subTest(client=client, shape=shape), \
                        tempfile.TemporaryDirectory() as out:
                    seen: list[str] = []

                    def claude(_question: str, config: str,
                               *_args: object) -> tuple[str, score.Run]:
                        config_data = json.loads(pathlib.Path(config).read_text())
                        seen.append(config_data["mcpServers"]["eshu"]["url"])
                        return "", run

                    def codex(_question: str, url: str,
                              *_args: object) -> tuple[str, score.Run]:
                        seen.append(url)
                        return "", run

                    flags = ["--client", client, "--out", out,
                             "--surfaces", "router", "--workers", "1"]
                    if shape is not None:
                        flags.extend(["--shape", shape])
                    env = {"ESHU_MCP_BASE_URL": "https://h/",
                           "ESHU_MCP_KEY": "secret"}
                    with mock.patch.dict(os.environ, env), \
                            mock.patch.object(run_eval, "load_tasks",
                                              return_value=[(task, "question")]), \
                            mock.patch.object(run_eval, "run_claude",
                                              side_effect=claude), \
                            mock.patch.object(run_eval, "run_codex",
                                              side_effect=codex), \
                            mock.patch.object(run_eval, "make_codex_home",
                                              return_value=out), \
                            mock.patch.object(run_eval, "codex_model",
                                              return_value="m"), \
                            mock.patch.object(run_eval, "client_version",
                                              return_value="v"), \
                            mock.patch.object(run_eval.score, "score",
                                              return_value=result):
                        self.assertEqual(run_eval.main(flags), 0)
                    suffix = "" if shape is None else f"&shape={shape}"
                    self.assertEqual(
                        seen, [f"https://h/mcp/message?surface=router{suffix}"])
                    saved = json.loads((pathlib.Path(out) /
                                        f"results_{client}.json").read_text())
                    expected_shape = shape if shape is not None else "server_default"
                    self.assertEqual(saved["meta"]["shape"], expected_shape)

    def test_invalid_shape_exits_before_creating_output(self) -> None:
        with tempfile.TemporaryDirectory() as out:
            destination = pathlib.Path(out) / "results"
            with self.assertRaises(SystemExit) as ctx:
                run_eval.main(["--out", str(destination), "--shape", "invalid"])
            self.assertEqual(ctx.exception.code, 2)
            self.assertFalse(destination.exists())

    def test_existing_other_shape_is_refused_before_running(self) -> None:
        with tempfile.TemporaryDirectory() as out:
            result = pathlib.Path(out) / "results_claude.json"
            result.write_text(json.dumps({"meta": {"client": "claude",
                                                   "shape": "dual"},
                                          "results": []}))
            original = result.read_text()
            with self.assertRaisesRegex(SystemExit, "separate private"):
                run_eval.main(["--out", out, "--shape", "structured"])
            self.assertEqual(result.read_text(), original)

    def test_corrupt_existing_results_fail_before_client_call(self) -> None:
        for contents in ("{broken", "[]", '{"meta": []}', '{}'):
            with self.subTest(contents=contents), \
                    tempfile.TemporaryDirectory() as out:
                result = pathlib.Path(out) / "results_claude.json"
                result.write_text(contents)
                with mock.patch.object(run_eval, "run_claude") as client:
                    with self.assertRaises(SystemExit) as ctx:
                        run_eval.main(["--out", out])
                self.assertIn(str(result), str(ctx.exception))
                self.assertEqual(result.read_text(), contents)
                client.assert_not_called()

    def test_unreadable_existing_results_fail_before_client_call(self) -> None:
        with tempfile.TemporaryDirectory() as out:
            result = pathlib.Path(out) / "results_claude.json"
            result.mkdir()
            with mock.patch.object(run_eval, "run_claude") as client:
                with self.assertRaises(SystemExit) as ctx:
                    run_eval.main(["--out", out])
            self.assertIn(str(result), str(ctx.exception))
            client.assert_not_called()

    def test_unusable_existing_rows_fail_before_client_call(self) -> None:
        valid = dict(score.Score("test-id", "code_lookup", 1, 1, 0,
                                 True, True, 1.0).__dict__, surface="router")
        cases = [
            {}, [],
            {key: value for key, value in valid.items() if key != "surface"},
            dict(valid, surface="unknown"),
            {key: value for key, value in valid.items() if key != "success"},
            dict(valid, tool_calls=True),
            dict(valid, tool_calls=10 ** 400),
            dict(valid, input_tokens=10 ** 400),
            dict(valid, wall_seconds=float("nan")),
        ]
        with tempfile.TemporaryDirectory() as out:
            result = pathlib.Path(out) / "results_claude.json"
            for row in cases:
                with self.subTest(row=row):
                    original = json.dumps({
                        "meta": {"client": "claude"}, "results": [row]})
                    result.write_text(original)
                    env = {"ESHU_MCP_BASE_URL": "https://h",
                           "ESHU_MCP_KEY": "test"}
                    with mock.patch.dict(os.environ, env), \
                            mock.patch.object(run_eval, "run_claude") as client:
                        with self.assertRaises(SystemExit) as ctx:
                            run_eval.main(["--out", out])
                    self.assertIn(str(result), str(ctx.exception))
                    self.assertEqual(result.read_text(), original)
                    client.assert_not_called()

    def test_integer_digit_limit_fails_before_client_or_overwrite(self) -> None:
        with tempfile.TemporaryDirectory() as out:
            result = pathlib.Path(out) / "results_claude.json"
            original = '{"meta": {"client": "claude"}, "results": [' + \
                       "9" * 5000 + "]}"
            result.write_text(original)
            env = {"ESHU_MCP_BASE_URL": "https://h", "ESHU_MCP_KEY": "test"}
            with mock.patch.dict(os.environ, env), \
                    mock.patch.object(run_eval, "run_claude") as client:
                with self.assertRaises(SystemExit) as ctx:
                    run_eval.main(["--out", out])
            self.assertIn(str(result), str(ctx.exception))
            self.assertEqual(result.read_text(), original)
            client.assert_not_called()

    def test_legacy_nonempty_results_allow_same_shape_run(self) -> None:
        row = dict(score.Score("test-id", "code_lookup", 1, 1, 0,
                               True, True, 1.0).__dict__, surface="router")
        for key in ("cost_usd", "input_tokens", "output_tokens"):
            del row[key]
        row["future_field"] = "ignored"
        with tempfile.TemporaryDirectory() as out:
            result = pathlib.Path(out) / "results_claude.json"
            result.write_text(json.dumps({
                "meta": {"client": "claude"}, "results": [row]}))
            with mock.patch.dict(os.environ, {
                    "ESHU_MCP_BASE_URL": "https://h", "ESHU_MCP_KEY": "test"}), \
                    mock.patch.object(run_eval, "client_version", return_value="v"), \
                    mock.patch.object(run_eval, "run_claude") as client:
                self.assertEqual(run_eval.main([
                    "--out", out, "--only", "none-such"]), 0)
            client.assert_not_called()
            self.assertEqual(json.loads(result.read_text())["meta"]["shape"],
                             "server_default")


if __name__ == "__main__":
    unittest.main()
