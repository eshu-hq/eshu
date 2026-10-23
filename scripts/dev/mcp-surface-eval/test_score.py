"""Tests for the hosted MCP surface-eval scoring module (issue #8760)."""

import json
import unittest

import score


def _claude_stream(*events: dict) -> str:
    """Joins Claude Code stream-json events into the JSON-lines text the CLI emits."""
    return "\n".join(json.dumps(e) for e in events)


def _tool_use(name: str, tool_input: dict, call_id: str) -> dict:
    return {
            "type": "assistant",
            "message": {"content": [{"type": "tool_use", "id": call_id, "name": name,
                                                              "input": tool_input}]},
    }


def _tool_result(call_id: str, is_error: bool = False) -> dict:
    return {
            "type": "user",
            "message": {"content": [{"type": "tool_result", "tool_use_id": call_id,
                                                              "is_error": is_error, "content": "x"}]},
    }


def _tool_result_text(call_id: str, text: str) -> dict:
    return {"type": "user", "message": {"content": [{
        "type": "tool_result", "tool_use_id": call_id, "content": text}]}}


def _result(text: str = "answer", duration_ms: int = 4000, turns: int = 3,
                        is_error: bool = False) -> dict:
    return {"type": "result", "subtype": "success", "is_error": is_error,
            "result": text, "duration_ms": duration_ms, "num_turns": turns,
            "total_cost_usd": 0.01,
            "usage": {"input_tokens": 4, "cache_creation_input_tokens": 100,
                      "cache_read_input_tokens": 900, "output_tokens": 50}}


TASK = score.Task(task_id="code-lookup-001", family="code_lookup",
                                    expected="find_symbol", alternates=("find_code",))


class ParseClaudeTest(unittest.TestCase):
    """parse_claude turns stream-json into ordered calls with wrapped tool names."""

    def test_unwraps_call_tool_and_keeps_direct_calls(self) -> None:
        stream = _claude_stream(
                _tool_use("mcp__eshu__find_tool", {"intent": "x"}, "a"),
                _tool_result("a"),
                _tool_use("mcp__eshu__call_tool",
                                    {"name": "find_symbol", "arguments": {}}, "b"),
                _tool_result("b"),
                _tool_use("mcp__eshu__find_code", {"query": "q"}, "c"),
                _tool_result("c", is_error=True),
                _result(),
        )
        run = score.parse_claude(stream)
        self.assertEqual([c.name for c in run.calls],
                                          ["find_tool", "call_tool", "find_code"])
        self.assertEqual([c.target for c in run.calls],
                                          ["find_tool", "find_symbol", "find_code"])
        self.assertEqual([c.is_error for c in run.calls], [False, False, True])
        self.assertEqual(run.wall_seconds, 4.0)
        self.assertEqual(run.final_text, "answer")
        self.assertEqual(run.cost_usd, 0.01)
        self.assertEqual(run.input_tokens, 1004)
        self.assertEqual(run.output_tokens, 50)

    def test_ignores_non_eshu_tools(self) -> None:
        stream = _claude_stream(_tool_use("Bash", {"command": "ls"}, "z"),
                                                        _tool_result("z"), _result())
        run = score.parse_claude(stream)
        self.assertEqual(run.calls, [])
        self.assertEqual(run.non_mcp_calls, 1)

    def test_tolerates_garbage_lines(self) -> None:
        run = score.parse_claude("not json\n" + _claude_stream(_result()))
        self.assertEqual(run.calls, [])
        self.assertEqual(run.final_text, "answer")


def _codex_stream(*events: dict) -> str:
    """Joins Codex ``exec --json`` events into JSON lines."""
    return "\n".join(json.dumps(e) for e in events)


def _codex_call(tool: str, arguments: dict, status: str = "completed",
                server: str = "eshu") -> dict:
    return {"type": "item.completed", "item": {
        "type": "mcp_tool_call", "server": server, "tool": tool,
        "arguments": arguments, "status": status,
        "error": None if status == "completed" else {"message": "boom"}}}


class ParseCodexTest(unittest.TestCase):
    """parse_codex reads Codex JSONL events into the same Run shape."""

    def test_unwraps_call_tool_and_reads_usage(self) -> None:
        stream = _codex_stream(
            {"type": "item.started", "item": {"type": "mcp_tool_call",
                                              "server": "eshu",
                                              "tool": "find_tool"}},
            _codex_call("find_tool", {"intent": "x"}),
            _codex_call("call_tool",
                        {"name": "find_symbol", "arguments": {}}),
            _codex_call("find_code", {"query": "q"}, status="failed"),
            {"type": "item.completed", "item": {"type": "agent_message",
                                                "text": "first"}},
            {"type": "item.completed", "item": {"type": "agent_message",
                                                "text": "final answer"}},
            {"type": "turn.completed", "usage": {
                "input_tokens": 1000, "cached_input_tokens": 800,
                "output_tokens": 40}},
        )
        run = score.parse_codex(stream)
        self.assertEqual([c.target for c in run.calls],
                         ["find_tool", "find_symbol", "find_code"])
        self.assertEqual([c.is_error for c in run.calls], [False, False, True])
        self.assertEqual(run.final_text, "final answer")
        self.assertEqual(run.input_tokens, 1000)
        self.assertEqual(run.output_tokens, 40)
        self.assertFalse(run.run_error)

    def test_ignores_other_servers_and_counts_shell(self) -> None:
        stream = _codex_stream(
            _codex_call("send", {}, server="other"),
            {"type": "item.completed", "item": {"type": "command_execution",
                                                "command": "ls"}},
        )
        run = score.parse_codex(stream)
        self.assertEqual(run.calls, [])
        self.assertEqual(run.non_mcp_calls, 2)

    def test_turn_failed_marks_run_error(self) -> None:
        run = score.parse_codex(_codex_stream(
            {"type": "turn.failed", "error": {"message": "x"}}))
        self.assertTrue(run.run_error)

    def test_garbage_lines_are_skipped(self) -> None:
        run = score.parse_codex("not json\n" + _codex_stream(
            {"type": "turn.completed", "usage": {}}))
        self.assertEqual(run.calls, [])
        self.assertFalse(run.run_error)


class OverflowTest(unittest.TestCase):
    """A result the client could not read is not an answer."""

    OVERFLOW = ("Error: result (240,698 characters) exceeds maximum allowed "
                "tokens. Output has been saved to /x/y.txt.")

    def test_parse_marks_overflowed_calls(self) -> None:
        stream = _claude_stream(
            _tool_use("mcp__eshu__find_symbol", {}, "a"),
            _tool_result_text("a", self.OVERFLOW),
            _tool_use("mcp__eshu__find_code", {}, "b"),
            _tool_result_text("b", "ok"),
            _result())
        run = score.parse_claude(stream)
        self.assertEqual([c.overflowed for c in run.calls], [True, False])

    def test_overflowed_expected_call_is_not_success(self) -> None:
        run = score.Run(
            calls=[score.Call("find_symbol", "find_symbol", overflowed=True)],
            final_text="answer", wall_seconds=1.0, turns=1, run_error=False)
        s = score.score(TASK, run)
        self.assertFalse(s.success)
        self.assertEqual(s.overflowed_calls, 1)

    def test_a_later_usable_call_still_succeeds(self) -> None:
        run = score.Run(
            calls=[score.Call("find_symbol", "find_symbol", overflowed=True),
                   score.Call("find_code", "find_code")],
            final_text="answer", wall_seconds=1.0, turns=2, run_error=False)
        self.assertTrue(score.score(TASK, run).success)


class WrongKindTest(unittest.TestCase):
    """Wrong-tool calls split into helper, sibling, and off-family."""

    TASK = score.Task(task_id="t", family="dead_code",
                      expected="find_dead_code", alternates=(),
                      family_tools=frozenset({"find_dead_code",
                                              "investigate_dead_code"}))

    def _score(self, *targets: str) -> score.Score:
        run = score.Run(calls=[score.Call(t, t) for t in targets],
                        final_text="a", wall_seconds=1.0, turns=1,
                        run_error=False)
        return score.score(self.TASK, run)

    def test_helper_sibling_and_off_family(self) -> None:
        s = self._score("resolve_entity", "investigate_dead_code",
                        "list_cloud_resource_inventory", "find_dead_code")
        self.assertEqual(s.wrong_tool_calls, 3)
        self.assertEqual(s.helper_calls, 1)
        self.assertEqual(s.sibling_wrong_calls, 1)
        self.assertEqual(s.off_family_wrong_calls, 1)

    def test_correct_calls_count_in_none(self) -> None:
        s = self._score("find_dead_code")
        self.assertEqual((s.helper_calls, s.sibling_wrong_calls,
                          s.off_family_wrong_calls), (0, 0, 0))

    def test_summarize_reports_genuine_wrong_rate(self) -> None:
        s = self._score("resolve_entity", "investigate_dead_code",
                        "find_dead_code")
        out = score.summarize([s])
        self.assertEqual(out["helper_rate"], 1 / 3)
        self.assertEqual(out["genuine_wrong_rate"], 1 / 3)


class ScoreTest(unittest.TestCase):
    """score applies the issue's four per-task measures."""

    def _run(self, *targets: tuple, final: str = "answer") -> score.Run:
        calls = [score.Call(name="call_tool" if t[0] not in ("find_tool",) else t[0],
                                                target=t[0], is_error=t[1]) for t in targets]
        return score.Run(calls=calls, final_text=final, wall_seconds=2.0,
                                          turns=len(calls), run_error=False)

    def test_correct_path_with_discovery_hop(self) -> None:
        s = score.score(TASK, self._run(("find_tool", False), ("find_symbol", False)))
        self.assertEqual(s.tool_calls, 2)
        self.assertEqual(s.domain_calls, 1)
        self.assertEqual(s.wrong_tool_calls, 0)
        self.assertTrue(s.first_domain_call_correct)
        self.assertTrue(s.success)

    def test_alternate_counts_as_correct(self) -> None:
        s = score.score(TASK, self._run(("find_code", False)))
        self.assertEqual(s.wrong_tool_calls, 0)
        self.assertTrue(s.success)

    def test_wrong_tool_counted_and_first_try_false(self) -> None:
        s = score.score(TASK, self._run(("get_repository_stats", False),
                                                                        ("find_symbol", False)))
        self.assertEqual(s.wrong_tool_calls, 1)
        self.assertFalse(s.first_domain_call_correct)
        self.assertTrue(s.success)

    def test_error_from_expected_tool_is_not_success(self) -> None:
        s = score.score(TASK, self._run(("find_symbol", True)))
        self.assertFalse(s.success)

    def test_no_final_text_is_not_success(self) -> None:
        s = score.score(TASK, self._run(("find_symbol", False), final=""))
        self.assertFalse(s.success)

    def test_run_error_is_not_success(self) -> None:
        run = self._run(("find_symbol", False))
        run.run_error = True
        self.assertFalse(score.score(TASK, run).success)

    def test_no_calls_at_all(self) -> None:
        s = score.score(TASK, self._run())
        self.assertEqual(s.tool_calls, 0)
        self.assertFalse(s.first_domain_call_correct)
        self.assertFalse(s.success)


class SummarizeTest(unittest.TestCase):
    """summarize rolls task scores up per surface without hiding failures."""

    def test_rates_and_means(self) -> None:
        a = score.Score(task_id="a", family="f", tool_calls=2, domain_calls=1,
                        wrong_tool_calls=0, first_domain_call_correct=True,
                        success=True, wall_seconds=10.0, cost_usd=0.02,
                        input_tokens=1000, output_tokens=100)
        b = score.Score(task_id="b", family="f", tool_calls=4, domain_calls=3,
                        wrong_tool_calls=2, first_domain_call_correct=False,
                        success=False, wall_seconds=30.0, cost_usd=0.04,
                        input_tokens=3000, output_tokens=300)
        out = score.summarize([a, b])
        self.assertEqual(out["tasks"], 2)
        self.assertEqual(out["mean_tool_calls"], 3.0)
        self.assertEqual(out["wrong_tool_rate"], 2 / 4)
        self.assertEqual(out["success_rate"], 0.5)
        self.assertEqual(out["first_try_rate"], 0.5)
        self.assertEqual(out["median_wall_seconds"], 20.0)
        self.assertAlmostEqual(out["mean_cost_usd"], 0.03)
        self.assertEqual(out["mean_input_tokens"], 2000.0)
        self.assertEqual(out["mean_output_tokens"], 200.0)


if __name__ == "__main__":
    unittest.main()
