"""Tests for the hosted MCP surface-eval report generator (issue #8760)."""

import contextlib
import io
import json
import pathlib
import tempfile
import unittest

import report


def _row(surface: str, task_id: str, ok: bool, calls: int = 2, wrong: int = 0,
         repeat: int = 0) -> dict:
    return {"surface": surface, "task_id": task_id, "family": "code_lookup",
            "tool_calls": calls, "domain_calls": max(calls - 1, 0),
            "wrong_tool_calls": wrong, "first_domain_call_correct": ok,
            "success": ok, "wall_seconds": 10.0, "cost_usd": 0.02,
            "input_tokens": 1000, "output_tokens": 100, "repeat": repeat}


DATA = {
    "claude": {
        "meta": {"client": "claude", "version": "1.2.3", "model": "m"},
        "results": [
            _row("full", "code-lookup-001", True),
            _row("full", "code-lookup-001", False, wrong=1, repeat=1),
            _row("router", "code-lookup-001", True, calls=3),
        ],
    },
}


class SummaryTableTest(unittest.TestCase):
    """summary_table gives one row per client and surface."""

    def test_rows_and_values(self) -> None:
        text = report.summary_table(DATA)
        self.assertIn("| claude | full | 2 |", text)
        self.assertIn("| claude | router | 1 |", text)
        self.assertIn("50%", text)
        self.assertIn("Genuine wrong-pick rate", text)
        self.assertIn("Helper-call rate", text)

    def test_surface_order_is_fixed(self) -> None:
        text = report.summary_table(DATA)
        self.assertLess(text.index("| full |"), text.index("| router |"))


class CostColumnTest(unittest.TestCase):
    """A client that reports no cost shows n/a, not a misleading 0.000."""

    def test_zero_cost_client_shows_not_available(self) -> None:
        rows = [dict(_row("full", "code-lookup-001", True), cost_usd=0.0)]
        data = {"codex": {"meta": {"client": "codex"}, "results": rows}}
        self.assertIn("| n/a |", report.summary_table(data))

    def test_reported_cost_is_formatted(self) -> None:
        self.assertIn("0.020", report.summary_table(DATA))


class TaskTableTest(unittest.TestCase):
    """task_table shows per task outcomes without any question text."""

    def test_shows_fraction_of_repeats_succeeding(self) -> None:
        text = report.task_table(DATA, {"code-lookup-001": "find_symbol"})
        self.assertIn("code-lookup-001", text)
        self.assertIn("find_symbol", text)
        self.assertIn("1/2", text)
        self.assertIn("1/1", text)


class MetaTest(unittest.TestCase):
    """clients_table records versions for the evidence note."""

    def test_lists_version_and_model(self) -> None:
        text = report.clients_table(DATA)
        self.assertIn("1.2.3", text)
        self.assertIn("| claude |", text)

    def test_lists_explicit_shape(self) -> None:
        data = {"claude": {"meta": {"client": "claude", "shape": "structured"},
                           "results": []}}
        self.assertIn("| claude | structured |", report.clients_table(data))

    def test_legacy_result_is_labeled_server_default(self) -> None:
        self.assertIn("| claude | server_default |", report.clients_table(DATA))


class InputShapeTest(unittest.TestCase):
    """Report inputs must not hide or combine different shapes."""

    def test_accepts_two_clients_with_same_shape(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            paths = []
            for client in ("claude", "codex"):
                path = pathlib.Path(directory) / f"{client}.json"
                path.write_text(json.dumps({"meta": {"client": client,
                                                      "shape": "structured"},
                                            "results": []}))
                paths.append(str(path))
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(report.main(paths), 0)
            self.assertIn("| claude | structured |", output.getvalue())
            self.assertIn("| codex | structured |", output.getvalue())

    def test_rejects_duplicate_client_with_different_shapes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            paths = []
            for shape in ("dual", "structured"):
                path = pathlib.Path(directory) / f"{shape}.json"
                path.write_text(json.dumps({"meta": {"client": "claude", "shape": shape},
                                            "results": []}))
                paths.append(str(path))
            with self.assertRaisesRegex(SystemExit, "shape"):
                with contextlib.redirect_stdout(io.StringIO()):
                    report.main(paths)

    def test_rejects_mixed_client_shapes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            paths = []
            for client, shape in (("claude", "dual"), ("codex", "structured")):
                path = pathlib.Path(directory) / f"{client}.json"
                path.write_text(json.dumps({"meta": {"client": client, "shape": shape},
                                            "results": []}))
                paths.append(str(path))
            with self.assertRaisesRegex(SystemExit, "shape"):
                with contextlib.redirect_stdout(io.StringIO()):
                    report.main(paths)

    def test_rejects_corrupt_input_with_path(self) -> None:
        contents_list = (
            "{broken", "[]", '{"meta": []}', '{"meta": {}}',
            '{"meta": {"client": []}}',
            '{"meta": {"client": "  "}, "results": []}',
            '{"meta": {"client": "claude"}}',
        )
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "corrupt.json"
            for contents in contents_list:
                with self.subTest(contents=contents):
                    path.write_text(contents)
                    with self.assertRaises(SystemExit) as ctx:
                        with contextlib.redirect_stdout(io.StringIO()):
                            report.main([str(path)])
                    self.assertIn(str(path), str(ctx.exception))

    def test_rejects_unreadable_input_with_path(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "unreadable.json"
            path.mkdir()
            with self.assertRaises(SystemExit) as ctx:
                report.main([str(path)])
            self.assertIn(str(path), str(ctx.exception))


class ResultRowBoundaryTest(unittest.TestCase):
    """Malformed result rows fail before any report table is printed."""

    def test_rejects_unusable_rows_without_partial_stdout(self) -> None:
        valid = _row("router", "code-lookup-001", True)
        cases = [
            {}, [],
            {key: value for key, value in valid.items() if key != "surface"},
            dict(valid, surface="unknown"),
            {key: value for key, value in valid.items() if key != "tool_calls"},
            dict(valid, tool_calls=True),
            dict(valid, wrong_tool_calls=-1),
            dict(valid, input_tokens=True),
            dict(valid, first_domain_call_correct="yes"),
            dict(valid, success="yes"),
            dict(valid, task_id=[]),
            dict(valid, family=[]),
            dict(valid, wall_seconds=float("nan")),
            dict(valid, cost_usd=float("inf")),
            dict(valid, helper_calls=True),
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "rows.json"
            for row in cases:
                with self.subTest(row=row):
                    path.write_text(json.dumps({
                        "meta": {"client": "claude"}, "results": [row]}))
                    output = io.StringIO()
                    with self.assertRaises(SystemExit) as ctx:
                        with contextlib.redirect_stdout(output):
                            report.main([str(path)])
                    self.assertIn(str(path), str(ctx.exception))
                    self.assertEqual(output.getvalue(), "")

    def test_accepts_legacy_optional_defaults_and_extra_fields(self) -> None:
        row = _row("router", "code-lookup-001", True)
        for key in ("cost_usd", "input_tokens", "output_tokens"):
            del row[key]
        row["future_field"] = "ignored"
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "legacy.json"
            path.write_text(json.dumps({
                "meta": {"client": "claude"}, "results": [row]}))
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(report.main([str(path)]), 0)
            self.assertIn("| claude | server_default |", output.getvalue())
            self.assertIn("| claude | router | 1 |", output.getvalue())


if __name__ == "__main__":
    unittest.main()
