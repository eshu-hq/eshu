"""Tests for the hosted MCP surface-eval report generator (issue #8760)."""

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


if __name__ == "__main__":
    unittest.main()
